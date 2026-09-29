package duplicate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
)

// probeChunkSize is the size of each head/middle/tail probe.
const probeChunkSize = 1 << 20 // 1 MiB

// newHasher returns the content hasher used for both probe and full hashes.
//
// The implementation is deliberately swappable. To switch to BLAKE3 (the
// recommendation in the requirements - about 4x faster than SHA-256 on the
// same hardware) add the dependency and return blake3.New() here:
//
//	go get github.com/zeebo/blake3
//
//	func newHasher() hash.Hash { return blake3.New() }
//
// Nothing else in this package needs to change.
func newHasher() hash.Hash { return sha256.New() }

// ProbeOffsets returns the byte offsets probed for the fast signature.
// Small files (<= 3 MiB) are hashed in full, larger files get head, middle
// and tail probes only.
func ProbeOffsets(size int64) []int64 {
	if size <= int64(probeChunkSize)*3 {
		return []int64{0}
	}
	mid := size/2 - int64(probeChunkSize)/2
	tail := size - int64(probeChunkSize)
	return []int64{0, mid, tail}
}

// ProbeSignature hashes head + middle + tail of a file, so two 10 GB files
// can be compared without reading 20 GB from disk.
func ProbeSignature(ctx context.Context, path string, size int64) (string, error) {
	sig, _, err := probeSignature(ctx, path, size)
	return sig, err
}

func probeSignature(ctx context.Context, path string, size int64) (string, int64, error) {
	if size <= 0 {
		return "", 0, os.ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := newHasher()
	var readBytes int64
	for _, off := range ProbeOffsets(size) {
		if err := ctx.Err(); err != nil {
			return "", readBytes, err
		}
		n := int64(probeChunkSize)
		if size <= int64(probeChunkSize)*3 {
			n = size // small files are verified in full
		}
		if rem := size - off; rem < n {
			n = rem
		}
		if n <= 0 {
			continue
		}
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return "", readBytes, err
		}
		read, err := io.CopyN(h, f, n)
		readBytes += read
		if err != nil {
			return "", readBytes, err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), readBytes, nil
}

// FullSignature hashes the complete file content.
func FullSignature(ctx context.Context, path string) (string, error) {
	sig, _, err := fullSignature(ctx, path, nil)
	return sig, err
}

func fullSignature(ctx context.Context, path string, onRead func(int64)) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := newHasher()
	var readBytes int64
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", readBytes, err
		}
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			readBytes += int64(n)
			if onRead != nil {
				onRead(int64(n))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", readBytes, err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), readBytes, nil
}
