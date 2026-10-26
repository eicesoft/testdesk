package scripts

import (
	"errors"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.Save(Script{Name: "login", Language: Python, Code: "print(1)"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("expected id and createdAt to be set, got %+v", created)
	}

	created.Code = "print(2)"
	updated, err := store.Save(created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("createdAt changed on update")
	}

	got, err := store.Get(created.ID)
	if err != nil || got.Code != "print(2)" {
		t.Fatalf("get: %v, code=%q", err, got.Code)
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, len=%d", err, len(list))
	}

	if err := store.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestStoreRejectsBadInput(t *testing.T) {
	store, _ := NewStore(t.TempDir())

	if _, err := store.Save(Script{Name: "", Language: Python}); err == nil {
		t.Error("empty name should be rejected")
	}
	if _, err := store.Save(Script{Name: "x", Language: "ruby"}); err == nil {
		t.Error("unsupported language should be rejected")
	}
	if _, err := store.Get("../etc/passwd"); err == nil {
		t.Error("path-like id should be rejected")
	}
}
