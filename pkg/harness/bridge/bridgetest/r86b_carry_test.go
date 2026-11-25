package bridgetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// The fake sandbox implements the optional SandboxSession extensions (TS
// SandboxSession stat / listDirectory / deleteFile) over its file map, with
// the providerutils contracts: (nil, nil) for a missing path, an error for
// the wrong kind of path.
func TestR86bcarrySandboxOptionalOperations(t *testing.T) {
	ctx := context.Background()
	var sb providerutils.SandboxSession = NewSandbox()
	s := sb.(*Sandbox)
	s.SetFile("/state/bridge-meta.json", `{"port":1}`)
	s.SetFile("/state/logs/out.txt", "x")
	s.SetFile("/top.txt", "")

	statter, ok := sb.(providerutils.SandboxStatter)
	if !ok {
		t.Fatal("Sandbox does not implement SandboxStatter")
	}
	for p, want := range map[string]*providerutils.SandboxFileStat{
		"/state/bridge-meta.json":   {Type: "file", Size: 10},
		"/state/./bridge-meta.json": {Type: "file", Size: 10},
		"/state":                    {Type: "directory"},
		"/state/logs/":              {Type: "directory"},
		"/":                         {Type: "directory"},
		"/missing":                  nil,
		"/sta":                      nil,
	} {
		got, err := statter.Stat(ctx, p)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Stat(%q) = %+v, %v; want %+v", p, got, err, want)
		}
	}

	lister, ok := sb.(providerutils.SandboxDirectoryLister)
	if !ok {
		t.Fatal("Sandbox does not implement SandboxDirectoryLister")
	}
	got, err := lister.ListDirectory(ctx, "/state")
	want := []providerutils.SandboxDirectoryEntry{{Name: "bridge-meta.json", Type: "file"}, {Name: "logs", Type: "directory"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ListDirectory(/state) = %+v, %v; want %+v", got, err, want)
	}
	got, err = lister.ListDirectory(ctx, "/")
	want = []providerutils.SandboxDirectoryEntry{{Name: "state", Type: "directory"}, {Name: "top.txt", Type: "file"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ListDirectory(/) = %+v, %v; want %+v", got, err, want)
	}
	if got, err := lister.ListDirectory(ctx, "/missing"); got != nil || err != nil {
		t.Errorf("ListDirectory(/missing) = %+v, %v; want nil, nil", got, err)
	}
	if _, err := lister.ListDirectory(ctx, "/top.txt"); err == nil {
		t.Error("ListDirectory on a file: want an error")
	}

	deleter, ok := sb.(providerutils.SandboxFileDeleter)
	if !ok {
		t.Fatal("Sandbox does not implement SandboxFileDeleter")
	}
	if ok, err := deleter.DeleteFile(ctx, "/state/logs/out.txt"); !ok || err != nil {
		t.Errorf("DeleteFile(existing) = %v, %v; want true, nil", ok, err)
	}
	if _, present := s.File("/state/logs/out.txt"); present {
		t.Error("deleted file still present")
	}
	if ok, err := deleter.DeleteFile(ctx, "/state/logs/out.txt"); ok || err != nil {
		t.Errorf("DeleteFile(missing) = %v, %v; want false, nil", ok, err)
	}
	if _, err := deleter.DeleteFile(ctx, "/state"); err == nil {
		t.Error("DeleteFile on a directory: want an error")
	}
	if st, _ := statter.Stat(ctx, "/state/logs"); st != nil {
		t.Errorf("Stat(/state/logs) after its last file was deleted = %+v, want nil", st)
	}
}
