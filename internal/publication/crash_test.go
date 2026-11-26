package publication

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"static-host/internal/store"
)

func TestCrashRecovery(t *testing.T) {
	if dir := os.Getenv("STATIC_HOST_CRASH_TEST_DIR"); dir != "" {
		db, err := store.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(db, dir, Options{})
		if err != nil {
			t.Fatal(err)
		}
		target := os.Getenv("STATIC_HOST_CRASH_TEST_POINT")
		s.failpoint = func(point string) {
			if point == target {
				os.Exit(91)
			}
		}
		_, err = s.Publish(context.Background(), Identity{OwnerID: 1, KeyID: "key"}, "s-test", strings.Repeat("b", 32), 1, "crash-request")
		t.Fatalf("failpoint not reached: %v", err)
	}
	for _, point := range []string{"before_rename", "after_rename", "after_commit"} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			old := strings.Repeat("a", 32)
			next := strings.Repeat("b", 32)
			for _, item := range []struct{ area, id, text string }{{"content", old, "old"}, {"staging", next, "new"}} {
				p := filepath.Join(dir, "sites", item.area, item.id)
				if err = os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(p, "index.html"), []byte(item.text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'agent',x'01',1)"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("INSERT INTO sites(id,owner_id,name,generation,current_dir,size_bytes,file_count,created_at,updated_at) VALUES('s-test',1,'site',1,?,3,1,1,1)", old); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("INSERT INTO uploads(id,site_id,owner_id,key_id,format,expected_generation,token_hash,state,expires_at,dir,size_bytes,file_count) VALUES(?,'s-test',1,'key','zip',1,x'01','ready',4102444800,?,3,1)", next, next); err != nil {
				t.Fatal(err)
			}
			db.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecovery$")
			cmd.Env = append(os.Environ(), "STATIC_HOST_CRASH_TEST_DIR="+dir, "STATIC_HOST_CRASH_TEST_POINT="+point)
			err = cmd.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 91 {
				t.Fatalf("child: %v", err)
			}
			db, err = store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s, err := New(db, dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var current string
			var generation int
			if err = db.QueryRow("SELECT current_dir,generation FROM sites WHERE id='s-test'").Scan(&current, &generation); err != nil {
				t.Fatal(err)
			}
			expected, expectedGeneration, content := old, 1, "old"
			if point == "after_commit" {
				expected, expectedGeneration, content = next, 2, "new"
			}
			if current != expected || generation != expectedGeneration {
				t.Fatalf("pointer=%s generation=%d", current, generation)
			}
			data, err := os.ReadFile(filepath.Join(dir, "sites", "content", current, "index.html"))
			if err != nil || string(data) != content {
				t.Fatalf("content=%q err=%v", data, err)
			}
			orphan := next
			if point == "after_commit" {
				orphan = old
			}
			if _, err = os.Stat(filepath.Join(dir, "sites", "content", orphan)); !os.IsNotExist(err) {
				t.Fatalf("orphan content remains: %v", err)
			}
		})
	}
}
