package main

import (
	"reflect"
	"testing"
)

// A field of App with the name of a localSession field would shadow the
// promoted one, and code written for the session would silently read App's.
func TestAppAndLocalSessionShareNoFieldNames(t *testing.T) {
	session := reflect.TypeFor[localSession]()
	app := reflect.TypeFor[App]()
	embedded, ok := app.FieldByName("localSession")
	if !ok || !embedded.Anonymous || embedded.Type != reflect.PointerTo(session) {
		t.Fatalf("App must embed *localSession, got %+v", embedded)
	}
	sessionFields := make(map[string]bool, session.NumField())
	for field := range session.Fields() {
		sessionFields[field.Name] = true
	}
	for field := range app.Fields() {
		if field.Anonymous && field.Type == reflect.PointerTo(session) {
			continue
		}
		if sessionFields[field.Name] {
			t.Errorf("App.%s shadows localSession.%s", field.Name, field.Name)
		}
	}
	// The config core and mu move together (#366).
	for _, name := range []string{"mu", "cfg", "cfgPath", "cfgLoaded", "cfgLoadError", "cfgBase", "configRevision"} {
		if !sessionFields[name] {
			t.Errorf("localSession has no field %s", name)
		}
	}
}

func TestAppConstructorsAllocateLocalSession(t *testing.T) {
	if NewApp().localSession == nil {
		t.Error("NewApp left localSession nil")
	}
	if newTestApp().localSession == nil {
		t.Error("newTestApp left localSession nil")
	}
}
