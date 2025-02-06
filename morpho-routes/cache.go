package morphoroutes

import (
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// in-memory cache that returns bytes corresponding to a requested path
type Cacher struct {
	directory map[string][]byte
}

var GlobalCache *Cacher

// Initializes the cache.
func (c *Cacher) InitCache() {
	c.directory = make(map[string][]byte)
}

// Fetches and returns cached content associated with a uri.
//
// Returns content and true if present, else nil and false.
func (c *Cacher) GetCached(uri string) ([]byte, bool) {
	item, ok := c.directory[uri]
	if ok {
		return item, true
	} else {
		return nil, false
	}
}

// Caches content associated with a uri.
func (c *Cacher) Cache(uri string, content []byte) {
	c.directory[uri] = content
}

// Middleware to cache responses.
func CacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contents, present := GlobalCache.GetCached(r.URL.Path)
		if !present {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(contents)
		log.Printf("used cached %s %s; response time: %dms\n", r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}
