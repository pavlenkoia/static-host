package publication

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchives(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				paths []string
				valid bool
			}{{"valid", []string{"index.html", "assets/a.css"}, true}, {"traversal", []string{"index.html", "../escape"}, false}, {"duplicate", []string{"index.html", "index.html"}, false}, {"collision", []string{"index.html", "assets/file", "assets"}, false}, {"missing index", []string{"asset"}, false}} {
				t.Run(tc.name, func(t *testing.T) {
					dir := t.TempDir()
					archive := filepath.Join(dir, "archive")
					f, err := os.Create(archive)
					if err != nil {
						t.Fatal(err)
					}
					if format == "zip" {
						w := zip.NewWriter(f)
						for _, name := range tc.paths {
							file, e := w.Create(name)
							if e != nil {
								t.Fatal(e)
							}
							if _, e = file.Write([]byte("x")); e != nil {
								t.Fatal(e)
							}
						}
						if err = w.Close(); err != nil {
							t.Fatal(err)
						}
					} else {
						gz := gzip.NewWriter(f)
						w := tar.NewWriter(gz)
						for _, name := range tc.paths {
							if err = w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: 1}); err != nil {
								t.Fatal(err)
							}
							if _, err = w.Write([]byte("x")); err != nil {
								t.Fatal(err)
							}
						}
						if err = w.Close(); err != nil {
							t.Fatal(err)
						}
						if err = gz.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if err = f.Close(); err != nil {
						t.Fatal(err)
					}
					root := filepath.Join(dir, "output")
					if err = os.Mkdir(root, 0700); err != nil {
						t.Fatal(err)
					}
					_, _, err = extractArchive(archive, format, root)
					if (err == nil) != tc.valid {
						t.Fatalf("valid=%v error=%v", tc.valid, err)
					}
				})
			}
		})
	}
}

func TestRejectTarTrailingData(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "index.html", Mode: 0600, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, "x"); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	data.WriteString("junk")
	if err := os.WriteFile(archive, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "out")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := extractArchive(archive, "tar.gz", root); err == nil {
		t.Fatal("accepted trailing data")
	}
}

func TestRejectArchiveLinks(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "archive")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			if format == "zip" {
				w := zip.NewWriter(f)
				h := &zip.FileHeader{Name: "index.html"}
				h.SetMode(os.ModeSymlink | 0777)
				entry, e := w.CreateHeader(h)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = io.WriteString(entry, "target"); e != nil {
					t.Fatal(e)
				}
				if e = w.Close(); e != nil {
					t.Fatal(e)
				}
			} else {
				gz := gzip.NewWriter(f)
				w := tar.NewWriter(gz)
				if err = w.WriteHeader(&tar.Header{Name: "index.html", Typeflag: tar.TypeLink, Linkname: "target"}); err != nil {
					t.Fatal(err)
				}
				if err = w.Close(); err != nil {
					t.Fatal(err)
				}
				if err = gz.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, "out")
			if err = os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			if _, _, err = extractArchive(archive, format, root); err == nil {
				t.Fatal("accepted link")
			}
		})
	}
}

func TestRejectCorruptZIPChecksum(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.CreateHeader(&zip.FileHeader{Name: "index.html", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("unique-content")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	pos := bytes.Index(data, []byte("unique-content"))
	if pos < 0 {
		t.Fatal("fixture content missing")
	}
	data[pos] ^= 1
	if err = os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "out")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err = extractArchive(archive, "zip", root); err == nil {
		t.Fatal("accepted corrupt checksum")
	}
}

func TestRejectTarMissingEndBlocks(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "index.html", Mode: 0600, Size: 512}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(bytes.Repeat([]byte("x"), 512)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "out")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := extractArchive(archive, "tar.gz", root); err == nil {
		t.Fatal("accepted tar without end blocks")
	}
}

func TestExtractorLimits(t *testing.T) {
	if maxArchiveBytes != 100<<20 || maxExpandedBytes != 250<<20 || maxFiles != 10000 || maxEntries != 20000 {
		t.Fatal("production limits changed")
	}
	for _, tc := range []struct {
		name           string
		size           int64
		files, entries int
	}{{"expanded bytes", maxExpandedBytes - 2, 0, 0}, {"file count", 0, maxFiles, 0}, {"entry count", 0, 0, maxEntries}} {
		t.Run(tc.name, func(t *testing.T) {
			x := &extractor{root: t.TempDir(), paths: map[string]bool{}, explicit: map[string]bool{}, size: tc.size, files: tc.files, entries: tc.entries}
			if err := x.add("index.html", false, bytes.NewBufferString("abc")); err == nil {
				t.Fatal("limit exceeded without error")
			}
		})
	}
}
