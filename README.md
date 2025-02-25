# par_crud
very basic crud server thats ment as an exrcies in learning go.

the main challange is keeping a valid connection graph concurently without leaking memory.
it is very easy to just forget to remove stale entries or lock things with a full write lock.
what we do is a compremise where we only ocationally clear some of the entries.

go's garbage collector is actually great for this. we can let it handle the very complex task of figuring out what memory can be freed and why. This sort of thing would be very hard to do in rust or C and we would probably have to just leak the memory or use an Arc (which is slower than go's Gc)

planned commands:
1. create \[name\] \[text\]
2. connect \[source\] \[dest\]
3. disconnect \[source\] \[dest\]
4. show \[name\]
5. remove \[name\]
6. update \[name\] \[text\]
