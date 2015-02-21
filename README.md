Store
=====

Yet another file backed element store, this time with more interfaces and consistency

To avoid a lot of casting from store.Element on Store#Get, you should probably
write a wrapper around this element store for each type you intend
to use it for. 

Pull requests welcome


    import "github.com/sebcat/store"


## Usage

```go
var (
	ErrAlreadyExists = errors.New("element already exists in store")
	ErrDoesNotExist  = errors.New("element not present in store")
)
```

#### type Cache

```go
type Cache interface {
	Cache(Element)
	Get(id ElementID) Element
}
```


#### type CacheMode

```go
type CacheMode int
```

Tells the store when to pass an element to the cache

```go
const (
	CacheOnGet CacheMode = (1 << 0)
	CacheOnPut           = (1 << 1)
)
```

#### type Element

```go
type Element interface {
	Load(io.Reader) error
	Store(io.Writer) error

	// must return a file-system safe ID
	// the two first letters of ID will be
	// used to create directories and the
	// complete ID string will be used as
	// a file name
	ID() ElementID
}
```


#### type ElementID

```go
type ElementID uint64
```


#### func (ElementID) String

```go
func (id ElementID) String() string
```

#### type LRUCache

```go
type LRUCache struct {
}
```

cache with LRU eviction policy

#### func  NewLRUCache

```go
func NewLRUCache(size int) *LRUCache
```
create a new cache

#### func (*LRUCache) Cache

```go
func (l *LRUCache) Cache(el Element)
```
update (insert, promote) an element in the cache

#### func (*LRUCache) Get

```go
func (l *LRUCache) Get(id ElementID) Element
```
retrieve an element from the cache, or nil if the element is not in cache

#### type Store

```go
type Store struct {
}
```


#### func  New

```go
func New(path string) (*Store, error)
```

#### func (*Store) Get

```go
func (s *Store) Get(element Element) (Element, error)
```
retrieve an Element either from cache or from disk. If retrieved from cache, the
cache Element will be returned. If retrieved from disk, the Element passed to
Get will be loaded with the data from disk. Therefor, this function should
always be called like:

    var el = SomeElement{ID: someID}
    ret, err := s.Get(&el)

#### func (*Store) Has

```go
func (s *Store) Has(id ElementID) bool
```

#### func (*Store) Put

```go
func (s *Store) Put(el Element) error
```

#### func (*Store) Remove

```go
func (s *Store) Remove() error
```

#### func (*Store) SetCache

```go
func (s *Store) SetCache(c Cache, mode CacheMode)
```

#### func (*Store) Sync

```go
func (s *Store) Sync()
```
