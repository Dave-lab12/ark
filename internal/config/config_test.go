package config

import (
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultConfigTOMLMatchesDefaultConfig(t *testing.T) {
	var got Config
	if _, err := toml.Decode(defaultConfigTOML, &got); err != nil {
		t.Fatalf("decode defaultConfigTOML: %v", err)
	}
	if err := got.normalize(); err != nil {
		t.Fatalf("normalize parsed defaultConfigTOML: %v", err)
	}

	want := DefaultConfig()
	if err := want.normalize(); err != nil {
		t.Fatalf("normalize DefaultConfig: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultConfigTOML drifted from DefaultConfig:\n got: %#v\nwant: %#v", got, want)
	}
}
