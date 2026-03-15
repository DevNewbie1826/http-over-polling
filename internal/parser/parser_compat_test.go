package parser

import "testing"

func TestParserStatusUsesReadyLabelsByMode(t *testing.T) {
	tests := []struct {
		name string
		kind ReqOrRsp
		want string
	}{
		{name: "request", kind: REQUEST, want: "startReq"},
		{name: "response", kind: RESPONSE, want: "startRsp"},
		{name: "both", kind: BOTH, want: "startReqOrRsp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.kind)
			if got := p.Status(); got != tt.want {
				t.Fatalf("Status() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParserResetRestoresReadyState(t *testing.T) {
	p := New(REQUEST)
	if _, err := p.Execute(&Setting{}, []byte("GET / HTTP/1.1\r\n\r\n")); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	p.Reset()

	if got := p.Status(); got != "startReq" {
		t.Fatalf("Status() after Reset = %q, want %q", got, "startReq")
	}
	if p.ReadyUpgradeData() {
		t.Fatal("ReadyUpgradeData() = true after Reset, want false")
	}
}

func TestParserEOFAfterCompletedResponseIsTrue(t *testing.T) {
	p := New(RESPONSE)
	if _, err := p.Execute(&Setting{}, []byte("HTTP/1.1 204 No Content\r\n\r\n")); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !p.EOF() {
		t.Fatal("EOF() = false after completed no-body response, want true")
	}
}

func TestParserUserDataRoundTrip(t *testing.T) {
	p := New(REQUEST)
	value := struct{ name string }{name: "ctx"}

	p.SetUserData(value)

	if got := p.GetUserData(); got != value {
		t.Fatalf("GetUserData() = %#v, want %#v", got, value)
	}
}

func TestParserGetUserDataDefaultsToNil(t *testing.T) {
	p := New(REQUEST)
	if got := p.GetUserData(); got != nil {
		t.Fatalf("GetUserData() = %#v, want nil", got)
	}
}

func TestParserReadyUpgradeDataRequiresCompleteUpgrade(t *testing.T) {
	p := New(REQUEST)
	if p.ReadyUpgradeData() {
		t.Fatal("ReadyUpgradeData() = true before upgrade completion, want false")
	}
	p.Upgrade = true
	if p.ReadyUpgradeData() {
		t.Fatal("ReadyUpgradeData() = true without completion, want false")
	}
	p.messageCompleteCalled = true
	if !p.ReadyUpgradeData() {
		t.Fatal("ReadyUpgradeData() = false after upgrade completion, want true")
	}
}
