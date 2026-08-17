package config

import "testing"

func TestConfigRequiresLoopbackAdminListener(t *testing.T) {
	if err := (Config{IPPListen: ":8631", AdminListen: "127.0.0.1:8080"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{IPPListen: ":8631", AdminListen: "0.0.0.0:8080"}).Validate(); err == nil {
		t.Fatal("remote admin listener should be rejected")
	}
}
