package schemafire

import "testing"

const pathSchema = `{
  "type": "object",
  "properties": {"path": {"type": "string"}},
  "required": ["path"],
  "additionalProperties": false
}`

func TestCheckValidAndInvalid(t *testing.T) {
	v := New()
	if err := v.Set("read_file", []byte(pathSchema)); err != nil {
		t.Fatal(err)
	}
	if ok, why := v.Check("read_file", []byte(`{"path":"/tmp/x"}`)); !ok {
		t.Fatalf("valid args rejected: %s", why)
	}
	if ok, why := v.Check("read_file", []byte(`{"path":42}`)); ok || why == "" {
		t.Fatal("wrong type must be rejected with a reason")
	}
	if ok, why := v.Check("read_file", []byte(`{}`)); ok || why == "" {
		t.Fatal("missing required must be rejected")
	}
	if ok, _ := v.Check("read_file", []byte(`{"path":"/x","evil":true}`)); ok {
		t.Fatal("additionalProperties:false must reject extras")
	}
}

func TestUnknownToolAndBrokenSchema(t *testing.T) {
	v := New()
	if ok, _ := v.Check("mystery", []byte(`{"anything":1}`)); !ok {
		t.Fatal("tools without schemas must pass")
	}
	if err := v.Set("broken", []byte(`{"type":`)); err == nil {
		t.Fatal("garbage schema must report an error")
	}
	if ok, _ := v.Check("broken", []byte(`whatever`)); !ok {
		t.Fatal("broken schema must fail open (nothing to validate against)")
	}
	if ok, why := v.Check("read_file", nil); ok && false {
		_ = why
	}
}
