package sys

import (
	"strings"
	"testing"
)

// Senza un processo Node la tabella è vuota: i wrapper devono restituire StatusUnavailable
// senza toccare le uscite né andare in crash.
func TestWrappersWithoutHost(t *testing.T) {
	sentinel := Value(nil)
	if st := CreateInt32(nil, 1, &sentinel); st != StatusUnavailable {
		t.Fatalf("CreateInt32 = %v, atteso StatusUnavailable", st)
	}
	if sentinel != nil {
		t.Fatal("l'uscita non deve essere scritta quando lo status non è StatusOK")
	}
	if st := CreateInt32(nil, 1, nil); st != StatusUnavailable {
		t.Fatalf("CreateInt32 con uscita nil = %v", st)
	}
	if st := Throw(nil, nil); st != StatusUnavailable {
		t.Fatalf("Throw = %v", st)
	}
	var n uint
	if st := GetCbInfo(nil, nil, &n, nil, nil, nil); st != StatusUnavailable {
		t.Fatalf("GetCbInfo = %v", st)
	}
}

func TestAvailableBeforeLoad(t *testing.T) {
	for f := Fn(0); f < fnCount; f++ {
		if Available(f) {
			t.Fatalf("%s risulta disponibile senza processo host", f)
		}
	}
	for _, bad := range []Fn{-1, fnCount, fnCount + 1} {
		if Available(bad) {
			t.Errorf("Available(%d) deve essere false", bad)
		}
	}
}

func TestFnMetadata(t *testing.T) {
	f, ok := FnByName("napi_create_int32")
	if !ok || f != FnCreateInt32 || f.MinVersion() != 1 || f.String() != "napi_create_int32" {
		t.Fatalf("napi_create_int32: %v %v %d", f, ok, f.MinVersion())
	}
	if FnSymbolFor.MinVersion() != 9 || FnCreatePropertyKeyUTF8.MinVersion() != 10 {
		t.Error("versioni minime errate per le funzioni Node-API 9/10")
	}
	if _, ok := FnByName("napi_inesistente"); ok {
		t.Error("FnByName deve fallire per un nome sconosciuto")
	}
	if _, ok := FnByName(""); ok {
		t.Error("FnByName deve fallire per un nome vuoto")
	}
	if got := Fn(-1).Name(); !strings.HasPrefix(got, "Fn(") || Fn(-1).MinVersion() != 0 {
		t.Errorf("Fn non valida: %q", got)
	}
	seen := map[string]bool{}
	for f := Fn(0); f < fnCount; f++ {
		if seen[f.Name()] {
			t.Errorf("nome duplicato %s", f.Name())
		}
		seen[f.Name()] = true
		if v := f.MinVersion(); v < 1 || v > MaxVersion {
			t.Errorf("%s: versione %d fuori da 1..%d", f, v, MaxVersion)
		}
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusOK:                  "napi_ok",
		StatusCannotRunJS:         "napi_cannot_run_js",
		StatusUnavailable:         "napi-go: function unavailable in this Node.js",
		Status(999):               "napi_status(999)",
		Status(-1):                "napi_status(-1)",
		StatusPendingException:    "napi_pending_exception",
		StatusInvalidArg:          "napi_invalid_arg",
		StatusArrayBufferExpected: "napi_arraybuffer_expected",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, atteso %q", int32(s), got, want)
		}
	}
	if TypeBigInt.String() != "napi_bigint" {
		t.Errorf("TypeBigInt.String() = %q", TypeBigInt.String())
	}
}

func TestDeclaredVersion(t *testing.T) {
	if got := ModuleAPIVersion(); got != int32(DeclaredVersion) {
		t.Fatalf("node_api_module_get_api_version_v1 = %d, DeclaredVersion = %d", got, DeclaredVersion)
	}
	if DeclaredVersion < 8 {
		t.Fatalf("DeclaredVersion = %d, minimo supportato 8", DeclaredVersion)
	}
}
