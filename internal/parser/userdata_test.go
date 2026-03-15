package parser

import "testing"

func TestSetUserDataAndGetUserData(t *testing.T) {
	p := New(REQUEST)

	p.SetUserData("test-data")
	if got := p.GetUserData(); got != "test-data" {
		t.Fatalf("GetUserData() = %v, want test-data", got)
	}

	p.SetUserData(map[string]int{"key": 42})
	data := p.GetUserData()
	if data == nil {
		t.Fatal("GetUserData() = nil, want map")
	}

	m := data.(map[string]int)
	if m["key"] != 42 {
		t.Fatalf("GetUserData()['key'] = %d, want 42", m["key"])
	}
}

func TestGetUserDataReturnsNilByDefault(t *testing.T) {
	p := New(REQUEST)
	if got := p.GetUserData(); got != nil {
		t.Fatalf("GetUserData() = %v, want nil for new parser", got)
	}
}
