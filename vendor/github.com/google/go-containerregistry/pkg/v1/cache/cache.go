// Package cache provides methods to cache layers.
package cache

import (
	"errors"
	"io"

	"github.com/google/go-containerregistry/pkg/logs"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Cache encapsulates methods to interact with cached layers.
type Cache interface {
	// Put writes the Layer to the Cache.
	//
	// The returned Layer should be used for future operations, since lazy
	// cachers might only populate the cache when the layer is actually
	// consumed.
	//
	// The returned layer can be consumed, and the cache entry populated,
	// by calling either Compressed or Uncompressed and consuming the
	// returned io.ReadCloser.
	Put(v1.Layer) (v1.Layer, error)

	// Get returns the Layer cached by the given Hash, or ErrNotFound if no
	// such layer was found.
	Get(v1.Hash) (v1.Layer, error)

	// Delete removes the Layer with the given Hash from the Cache.
	Delete(v1.Hash) error
}

// ErrNotFound is returned by Get when no layer with the given Hash is found.
var ErrNotFound = errors.New("layer was not found")

// Image returns a new Image which wraps the given Image, whose layers will be
// pulled from the Cache if they are found, and written to the Cache as they
// are read from the underlying Image.
func Image(i v1.Image, c Cache) v1.Image {
	return &image{
		Image: i,
		c:     c,
	}
}

type image struct {
	v1.Image
	c Cache
}

func (i *image) Layers() ([]v1.Layer, error) {
	ls, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}

	var out []v1.Layer
	for _, l := range ls {
		out = append(out, &lazyLayer{inner: l, c: i.c})
	}
	return out, nil
}

type lazyLayer struct {
	inner v1.Layer
	c     Cache
}

func (l *lazyLayer) Compressed() (io.ReadCloser, error) {
	digest, err := l.inner.Digest()
	if err != nil {
		return nil, err
	}

	if cl, err := l.c.Get(digest); err == nil {
		// Layer found in the cache.
		logs.Progress.Printf("Layer %s found (compressed) in cache", digest)
		return cl.Compressed()
	} else if err != ErrNotFound {
		return nil, err
	}

	// Not cached, pull and return the real layer.
	logs.Progress.Printf("Layer %s not found (compressed) in cache, getting", digest)
	rl, err := l.c.Put(l.inner)
	if err != nil {
		return nil, err
	}
	return rl.Compressed()
}

func (l *lazyLayer) Uncompressed() (io.ReadCloser, error) {
	diffID, err := l.inner.DiffID()
	if err != nil {
		return nil, err
	}
	if cl, err := l.c.Get(diffID); err == nil {
		// Layer found in the cache.
		logs.Progress.Printf("Layer %s found (uncompressed) in cache", diffID)
		return cl.Uncompressed()
	} else if err != ErrNotFound {
		return nil, err
	}

	// Not cached, pull and return the real layer.
	logs.Progress.Printf("Layer %s not found (uncompressed) in cache, getting", diffID)
	rl, err := l.c.Put(l.inner)
	if err != nil {
		return nil, err
	}
	return rl.Uncompressed()
}

func (l *lazyLayer) Size() (int64, error)                { return l.inner.Size() }
func (l *lazyLayer) DiffID() (v1.Hash, error)            { return l.inner.DiffID() }
func (l *lazyLayer) Digest() (v1.Hash, error)            { return l.inner.Digest() }
func (l *lazyLayer) MediaType() (types.MediaType, error) { return l.inner.MediaType() }

func (i *image) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	l, err := i.c.Get(h)
	if err == ErrNotFound {
		// Not cached, get it and write it.
		l, err := i.Image.LayerByDigest(h)
		if err != nil {
			return nil, err
		}
		return i.c.Put(l)
	}
	return l, err
}

func (i *image) LayerByDiffID(h v1.Hash) (v1.Layer, error) {
	l, err := i.c.Get(h)
	if err == ErrNotFound {
		// Not cached, get it and write it.
		l, err := i.Image.LayerByDiffID(h)
		if err != nil {
			return nil, err
		}
		return i.c.Put(l)
	}
	return l, err
}
