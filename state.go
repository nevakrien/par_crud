package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

const cleanupFreq uint64 = 100


type Node struct {
	dead           atomic.Bool
	lock           sync.RWMutex
	text           string
	name           string
	children       map[string]*atomic.Pointer[Node]
	cleanupCounter atomic.Uint64
}

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
// and triggers maybeCleanup. This is an internal helper.
func (node *Node) getAndResetDead(ptr *atomic.Pointer[Node]) *Node {
	child := ptr.Load()
	if child != nil && child.dead.Load() {
		if ptr.CompareAndSwap(child, nil) {
			newCount := node.cleanupCounter.Add(1)
			node.maybeCleanup(newCount)
		}
		return nil
	}
	return child
}

// child retrieves a child by name, using getAndResetDead.
func (node *Node) child(childName string) *Node {
	node.lock.RLock()
	ptr, exists := node.children[childName]
	node.lock.RUnlock()
	if !exists {
		return nil
	}
	return node.getAndResetDead(ptr)
}

// getValidChildren iterates over the entire children map (in a single loop)
// calling getAndResetDead for each pointer, and removes entries whose pointer is nil.
func (node *Node) getValidChildren() []*Node {
	node.lock.Lock()
	defer node.lock.Unlock()
	var valid []*Node
	for key, ptr := range node.children {
		child := node.getAndResetDead(ptr)
		if child == nil {
			delete(node.children, key)
		} else {
			valid = append(valid, child)
		}
	}
	return valid
}

// maybeCleanup performs a full cleanup of the children map if:
//    (counter % cleanupFreq == 0) && (counter > 2×(number of children))
// WARNING: This method acquires a write lock. Do not call it while holding a read lock.
func (node *Node) maybeCleanup(currentCount uint64) {
	if currentCount%cleanupFreq != 0 || currentCount <= 2*uint64(len(node.children)) {
		return
	}
	node.lock.Lock()
	defer node.lock.Unlock()
	currentCount = node.cleanupCounter.Load()
	if currentCount%cleanupFreq != 0 || currentCount <= 2*uint64(len(node.children)) {
		return
	}
	for key, ptr := range node.children {
		if ptr.Load() == nil {
			delete(node.children, key)
		}
	}
	delta := currentCount - (currentCount % cleanupFreq)
	// Subtract delta from the counter (this is equivalent to counter = counter % cleanupFreq).
	node.cleanupCounter.Add(^(delta - 1))
}

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
			ans += "\n - " + child.text
		}
	}
	return ans
}

func main() {
	st := &State{}

	st.create("A", "Parent Node")
	st.create("B", "Child Node 1")
	st.create("C", "Child Node 2")
	st.connect("A", "B")
	st.connect("A", "C")

	fmt.Println(st.show("A"))

	st.remove("B")
	fmt.Println("\nAfter removing Child Node 1:")
	fmt.Println(st.show("A"))
}
