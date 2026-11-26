package publication

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxArchiveBytes int64 = 100 << 20
const maxExpandedBytes int64 = 250 << 20
const maxFiles = 10000
const maxEntries = 20000

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

type extractor struct {
	root           string
	paths          map[string]bool
	explicit       map[string]bool
	entries, files int
	size           int64
	index          bool
}

func (x *extractor) add(name string, dir bool, src io.Reader) error {
	if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') || len(name) > 4096 {
		return failure("invalid_archive")
	}
	name = strings.TrimSuffix(name, "/")
	if !fs.ValidPath(name) || name == "." || strings.Count(name, "/")+1 > 64 {
		return failure("invalid_archive")
	}
	x.entries++
	if x.entries > maxEntries {
		return failure("invalid_archive")
	}
	if x.explicit[name] {
		return failure("invalid_archive")
	}
	if isDir, exists := x.paths[name]; exists && (!dir || !isDir) {
		return failure("invalid_archive")
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if isDir, ok := x.paths[parent]; ok && !isDir {
			return failure("invalid_archive")
		}
		x.paths[parent] = true
	}
	x.explicit[name] = true
	x.paths[name] = dir
	dest := filepath.Join(x.root, filepath.FromSlash(name))
	if dir {
		return os.MkdirAll(dest, 0700)
	}
	x.files++
	if x.files > maxFiles {
		return failure("invalid_archive")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(src, maxExpandedBytes-x.size+1))
	x.size += n
	closeErr := f.Close()
	if copyErr != nil {
		return failure("invalid_archive")
	}
	if closeErr != nil {
		return closeErr
	}
	if x.size > maxExpandedBytes {
		return failure("invalid_archive")
	}
	if name == "index.html" && n > 0 {
		x.index = true
	}
	return nil
}

func extractArchive(archive, format, root string) (int64, int64, error) {
	x := &extractor{root: root, paths: make(map[string]bool), explicit: make(map[string]bool)}
	var err error
	switch format {
	case "zip":
		var z *zip.ReadCloser
		z, err = zip.OpenReader(archive)
		if err != nil {
			return 0, 0, failure("invalid_archive")
		}
		defer z.Close()
		for _, entry := range z.File {
			mode := entry.Mode()
			if !mode.IsRegular() && !mode.IsDir() || mode.IsDir() && entry.UncompressedSize64 != 0 {
				return 0, 0, failure("invalid_archive")
			}
			var r io.ReadCloser
			r, err = entry.Open()
			if err != nil {
				break
			}
			err = x.add(entry.Name, mode.IsDir(), r)
			closeErr := r.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				break
			}
		}
	case "tar.gz":
		var f *os.File
		f, err = os.Open(archive)
		if err != nil {
			return 0, 0, err
		}
		defer f.Close()
		input := bufio.NewReader(f)
		var gz *gzip.Reader
		gz, err = gzip.NewReader(input)
		if err != nil {
			return 0, 0, failure("invalid_archive")
		}
		gz.Multistream(false)
		counted := &countingReader{reader: gz}
		tr := tar.NewReader(counted)
		for {
			before := counted.count
			var h *tar.Header
			h, err = tr.Next()
			if errors.Is(err, io.EOF) {
				if counted.count-before < 1024 {
					err = failure("invalid_archive")
				} else {
					err = nil
				}
				break
			}
			if err != nil {
				break
			}
			name := strings.TrimPrefix(h.Name, "./")
			if h.Typeflag == tar.TypeDir && h.Size != 0 {
				err = failure("invalid_archive")
				break
			}
			if name == "" && h.Typeflag == tar.TypeDir {
				continue
			}
			switch h.Typeflag {
			case tar.TypeReg, tar.TypeRegA:
				err = x.add(name, false, tr)
			case tar.TypeDir:
				err = x.add(name, true, tr)
			default:
				err = failure("invalid_archive")
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			var padding [4096]byte
			var total int
			for {
				n, e := gz.Read(padding[:])
				total += n
				if total > 1<<20 {
					err = failure("invalid_archive")
					break
				}
				for _, b := range padding[:n] {
					if b != 0 {
						err = failure("invalid_archive")
						break
					}
				}
				if err != nil {
					break
				}
				if errors.Is(e, io.EOF) {
					break
				}
				if e != nil {
					err = e
					break
				}
			}
		}
		if closeErr := gz.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			var b [1]byte
			n, e := input.Read(b[:])
			if n != 0 || e != io.EOF {
				err = failure("invalid_archive")
			}
		}
	default:
		return 0, 0, failure("invalid_request")
	}
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			return 0, 0, err
		}
		return 0, 0, failure("invalid_archive")
	}
	if !x.index {
		return 0, 0, failure("invalid_archive")
	}
	return x.size, int64(x.files), nil
}
