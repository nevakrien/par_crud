package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const cleanupFreq int64 = 100

type Node struct {
	dead           atomic.Bool
	lock           sync.RWMutex
	
	text           string
	name           string
	children       map[string]*atomic.Pointer[Node]
	
	cleanupCounter atomic.Int64 //used so we periodically clear the internal table (otherwise we leak memory)
}

// NewNode creates a new Node.
func NewNode(name, text string) *Node {
	return &Node{
		name:     name,
		text:     text,
		children: make(map[string]*atomic.Pointer[Node]),
	}
}

// addChild stores a child in the children map via an atomic pointer.
func (node *Node) addChild(child *Node) {
	node.lock.Lock()
	defer node.lock.Unlock()
	var ptr atomic.Pointer[Node]
	ptr.Store(child)
	node.children[child.name] = &ptr
}

// getAndResetDead checks if the given pointer's Node is dead.
// If dead, it atomically resets the pointer to nil, increments the cleanup counter,
// and returns (nil, true) if the cleanup condition is met.
func (node *Node) getAndResetDead(ptr *atomic.Pointer[Node]) (*Node, bool) {
	child := ptr.Load()
	if child != nil && child.dead.Load() {
		if ptr.CompareAndSwap(child, nil) {
			newCount := node.cleanupCounter.Add(1)
			if newCount%cleanupFreq == 0 && newCount > 2*int64(len(node.children)) {
				return nil, true
			}
		}
		return nil, false
	}
	return child, false
}

// conditionalCleanup calls cleanup if shouldCleanup is true.
func (node *Node) conditionalCleanup(shouldCleanup bool) {
	if shouldCleanup {
		node.cleanup()
	}
}

// cleanup performs a full cleanup of the children map under a write lock.
// WARNING: This method acquires a write lock. Do not call it while holding a read lock.
func (node *Node) cleanup() {
	node.lock.Lock()
	defer node.lock.Unlock()
	currentCount := node.cleanupCounter.Load()
	for key, ptr := range node.children {
		if ptr.Load() == nil {
			delete(node.children, key)
		}
	}
	delta := currentCount - (currentCount % cleanupFreq)
	node.cleanupCounter.Add(-delta)
}

// child retrieves a child by name using getAndResetDead.
func (node *Node) child(childName string) *Node {
	node.lock.RLock()
	ptr, exists := node.children[childName]
	node.lock.RUnlock()
	if !exists {
		return nil
	}
	child, cleanupNeeded := node.getAndResetDead(ptr)
	// Call conditionalCleanup after releasing the lock.
	node.conditionalCleanup(cleanupNeeded)
	return child
}

// getValidChildren iterates over the children map in a single loop,
// calling getAndResetDead for each pointer and deleting entries that become nil.
// It then conditionally cleans up.
// The deferred anonymous function ensures that conditionalCleanup is called after the lock is released.
func (node *Node) getValidChildren() []*Node {
	var cleanupNeeded bool = false
	// Call conditionalCleanup once after we release the lock
	defer func() {
		node.conditionalCleanup(cleanupNeeded)
	}()

	node.lock.RLock()
	defer node.lock.RUnlock()
	var valid []*Node
	for _, ptr := range node.children {
		child, needCleanup := node.getAndResetDead(ptr)
		if needCleanup {
			cleanupNeeded = true
		}
		if child !=nil {
			valid = append(valid, child)
		}
	}
	return valid
}

//
// State holds all live nodes. A node is marked dead only after removal from State.
//
type State struct {
	nodes sync.Map // map[string]*Node
}

func (state *State) create(name, text string) error {
	node := NewNode(name, text)
	if _, loaded := state.nodes.LoadOrStore(name, node); loaded {
		return errors.New("node already exists")
	}
	return nil
}

func (state *State) remove(name string) error {
	rawValue, loaded := state.nodes.LoadAndDelete(name)
	if !loaded {
		return errors.New("node does not exist")
	}
	removedNode := rawValue.(*Node)
	removedNode.dead.Store(true)
	return nil
}

func (state *State) get(name string) (*Node, bool) {
	rawValue, exists := state.nodes.Load(name)
	if !exists {
		return nil, false
	}
	return rawValue.(*Node), true
}

func (state *State) connect(parent, child string) error {
	parentNode, parentExists := state.get(parent)
	childNode, childExists := state.get(child)
	if !parentExists || !childExists {
		return errors.New("one or both nodes do not exist")
	}
	parentNode.addChild(childNode)
	return nil
}

func (state *State) show(name string) string {
	node, exists := state.get(name)
	if !exists {
		return name + " is empty"
	}
	ans := "Node: \"" + node.text + "\"\nChildren:"
	children := node.getValidChildren()
	if len(children) == 0 {
		ans += " None"
	} else {
		for _, child := range children {
			ans += "\n - " + child.name
		}
	}
	return ans
}

func main() {
	st := &State{}

	// Create parent and some children.
	st.create("A", "Parent Node")
	// Create 200 children for node A.
	for i := 1; i <= 200; i++ {
		childName := fmt.Sprintf("B%d", i)
		st.create(childName, fmt.Sprintf("Child Node %d", i))
		st.connect("A", childName)
	}

	fmt.Println("Before deletion, A's children:")
	fmt.Println(st.show("A"))

	// Remove all children from A to simulate garbage.
	for i := 1; i <= 200; i++ {
		childName := fmt.Sprintf("B%d", i)
		st.remove(childName)
	}

	// Allow some time for goroutines to run (if any) and cleanup to be triggered.
	time.Sleep(100 * time.Millisecond)

	fmt.Println("\nAfter removal of children, A's children (cleanup should trigger):")
	fmt.Println(st.show("A"))
}
