//
// concurrency-safe file-backed element store
//
//   - Asynchronous writes (errors reported on next action)
//   - Optional cache with LRU eviction on either
//     store insertion or store retrieval
//
// To avoid a lot of casting from store.Element on Store#Get, you should probably
// write a wrapper around this element store for each type you intend
// to use it for.
//
// Pull requests welcome
package store

import (
	"container/list"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var (
	ErrAlreadyExists = errors.New("element already exists in store")
	ErrDoesNotExist  = errors.New("element not present in store")
)

type ElementID uint64

func (id ElementID) String() string {
	return strconv.FormatUint(uint64(id), 36)
}

func (id *ElementID) FromString(str string) error {
	n, err := strconv.ParseUint(str, 36, 64)
	if err != nil {
		return err
	}
	*id = ElementID(n)
	return nil
}

type Element interface {
	// load an element from a reader
	Load(io.Reader) error

	// store an element to a writer
	Store(io.Writer) error

	// must return a file-system safe ID
	// the two first letters of ID will be
	// used to create directories and the
	// complete ID string will be used as
	// a file name
	ID() ElementID
}

type Cache interface {
	// update (insert, promote) an element in the cache
	Cache(Element)

	// retrieve an Element from the cache, or nil if non-existant
	Get(id ElementID) Element
}

// cache with LRU eviction policy
type LRUCache struct {
	l     *list.List
	m     map[ElementID]*list.Element
	mutex sync.Mutex
	size  int
}

// create a new cache with room for 'size' elements
func NewLRUCache(size int) *LRUCache {
	if size <= 0 {
		return nil
	}

	return &LRUCache{
		l:    list.New(),
		m:    make(map[ElementID]*list.Element),
		size: size,
	}
}

// update (insert, promote) an element in the cache
func (l *LRUCache) Cache(el Element) {
	// how cache with LRU eviction works:
	//
	// The cache itself is a linked list and a hash table.
	// new elements are put in the front of the list and
	// old ones are removed from the end back of the list.
	// The hash table is used for lookups in O(1) time
	//
	// If we assume a constant element size, this
	// implementation gives us constant space over time
	//
	// cache algorithm:
	//   if in cache:
	//     promote list element to front of list
	//   if not in cache:
	//     if cache not full:
	//       insert to front of list
	//       insert front element to hash table
	//     if cache is full:
	//       remove last element in list from hash table
	//       replace last element in list with new element
	//       move last element in list to front
	//       insert the now front element in list to hash table

	if l == nil || el == nil {
		return
	}

	id := el.ID()

	l.mutex.Lock()
	defer l.mutex.Unlock()

	listElem, exists := l.m[id]
	if exists {
		l.l.MoveToFront(listElem)
	} else {
		if len(l.m) < l.size {
			listElem = l.l.PushFront(el)
			l.m[id] = listElem
		} else {
			listElem = l.l.Back()
			listVal := listElem.Value.(Element)
			delete(l.m, listVal.ID())
			listElem.Value = el
			l.l.MoveToFront(listElem)
			l.m[id] = listElem
		}
	}
}

// retrieve an element from the cache, or nil if
// the element is not in cache
func (l *LRUCache) Get(id ElementID) Element {

	if l == nil {
		return nil
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	el, exists := l.m[id]
	if !exists {
		return nil
	}

	return el.Value.(Element)
}

// Tells the store when to pass an element
// to the cache
type CacheMode int

const (
	CacheOnGet CacheMode = (1 << 0)
	CacheOnPut           = (1 << 1)
)

type Store struct {
	path string

	cache     Cache
	cacheMode CacheMode

	inventory map[ElementID]struct{}
	ilock     sync.RWMutex

	inMem     map[ElementID]Element
	inMemLock sync.RWMutex

	writeErr     error
	activeWrites sync.WaitGroup
}

func New(path string) (*Store, error) {
	s := &Store{
		path:      filepath.Clean(path),
		inventory: make(map[ElementID]struct{}),
		inMem:     make(map[ElementID]Element),
	}

	os.MkdirAll(s.path, 0700)
	walker := func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			var id ElementID
			if err := id.FromString(info.Name()); err == nil {
				var x struct{}
				s.inventory[id] = x

			}
		}

		return err
	}

	if err := filepath.Walk(s.path, walker); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) SetCache(c Cache, mode CacheMode) {
	s.cache = c
	s.cacheMode = mode
}

func (s *Store) Has(id ElementID) bool {
	// we check inMem first, because the inventory
	// is updated before inMem is updated
	s.inMemLock.RLock()
	_, exists := s.inMem[id]
	s.inMemLock.RUnlock()
	if exists {
		return true
	}

	s.ilock.RLock()
	_, exists = s.inventory[id]
	s.ilock.RUnlock()
	return exists
}

func (s *Store) eldir(el Element) string {
	id := el.ID().String()
	if len(id) == 0 {
		panic("zero length ID size")
	}

	dirLen := 2
	if len(id) < dirLen {
		dirLen = len(id)
	}

	return filepath.Join(s.path, id[:dirLen])
}

func (s *Store) get(el Element) error {
	id := el.ID()
	path := filepath.Join(s.eldir(el), id.String())
	fh, err := os.Open(path)
	if err != nil {
		return err
	}

	defer fh.Close()
	return el.Load(fh)
}

// retrieve an Element either from cache or from disk.
// If retrieved from cache, the cache Element will be returned.
// If retrieved from disk, the Element passed to Get will be
// loaded with the data from disk. Therefor, this function
// should always be called like:
//   var el = SomeElement{ID: someID}
//   ret, err := s.Get(&el)
func (s *Store) Get(element Element) (Element, error) {
	id := element.ID()
	if s.cache != nil {
		if el := s.cache.Get(id); el != nil {
			return el, nil
		}
	}

	s.inMemLock.RLock()
	el, exists := s.inMem[id]
	s.inMemLock.RUnlock()
	if exists {
		return el, nil
	}

	// We check the inventory last because
	// an element is only added to the
	// inventory on a successful write.
	// it may be in write transfer cache (inMem)
	// and not in the inventory yet
	s.ilock.RLock()
	_, exists = s.inventory[id]
	s.ilock.RUnlock()
	if !exists {
		return nil, ErrDoesNotExist
	}

	err := s.get(element)
	if err != nil {
		return nil, err
	}

	if s.cache != nil && (s.cacheMode&CacheOnGet != 0) {
		s.cache.Cache(element)
	}

	return element, nil

}

func (s Store) put(el Element) error {
	dir := s.eldir(el)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, el.ID().String())
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	if err := el.Store(fh); err != nil {
		return err
	}

	return fh.Close()
}

func (s *Store) Put(el Element) error {

	if s.writeErr != nil {
		return s.writeErr
	}

	id := el.ID()
	if s.Has(id) {
		return ErrAlreadyExists
	}

	s.inMemLock.Lock()
	s.inMem[id] = el
	s.inMemLock.Unlock()

	s.activeWrites.Add(1)

	go func() {
		defer s.activeWrites.Done()

		defer func() {
			s.inMemLock.Lock()
			delete(s.inMem, id)
			s.inMemLock.Unlock()
		}()

		if err := s.put(el); err != nil {
			s.writeErr = err
		} else {
			var x struct{}
			s.ilock.Lock()
			s.inventory[id] = x
			s.ilock.Unlock()
			if s.cache != nil && (s.cacheMode&CacheOnPut) != 0 {
				s.cache.Cache(el)
			}
		}
	}()

	return nil
}

func (s *Store) Sync() {
	s.activeWrites.Wait()
}

func (s *Store) Remove() error {
	s.Sync()
	return os.RemoveAll(s.path)
}
