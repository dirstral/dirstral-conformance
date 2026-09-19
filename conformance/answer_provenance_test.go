package conformance_test

import (
	"encoding/json"
	"testing"
)

// SPEC 9.4.5, spec 0.70.0: answer provenance.
//
// This suite exists because spec/versioning.md 0.70.0 says it should:
//
//	`dirstral-conformance` SHOULD add a suite asserting the marking.
//
// It is the first entry in this repo to satisfy that obligation, which the
// spec's change process (step 5) has required of every version since 0.5.0.
//
// What is asserted here is the CONTRACT a client codes against, from outside:
// the fields are advertised, their vocabularies are closed and correct, and
// the pairing between them is stated in the served schema so a generated
// client can enforce it. Whether a given server sets them correctly when its
// generator fails needs a credentialed run with a broken provider, which is
// out of reach of a credential-free suite; that half is noted rather than
// faked.

// answerSurfaces are the tools 9.4.5 declares the fields on. A server MAY omit
// an optional tool, so a missing surface is skipped, not failed: the contract
// is about tools that exist.
var answerSurfaces = []string{
	"dir2mcp_ask",
	"dir2mcp_ask_audio",
	"dir2mcp_transcribe_and_ask",
}

func TestAnswerProvenance_FieldsAreAdvertised(t *testing.T) {
	srv := requireServer(t, corpus)
	tools := dial(t, srv).tools(t)

	seen := 0
	for _, name := range answerSurfaces {
		tool, ok := tools[name]
		if !ok {
			t.Logf("%s is not advertised; skipping (optional tool)", name)
			continue
		}
		seen++
		props := outputProperties(t, tool, name)

		source, ok := props["answer_source"].(map[string]interface{})
		if !ok {
			t.Errorf("%s output schema declares no answer_source (SPEC 9.4.5)", name)
			continue
		}
		assertEnum(t, name+".answer_source", source, "generated", "retrieval_only")

		reason, ok := props["answer_source_reason"].(map[string]interface{})
		if !ok {
			t.Errorf("%s output schema declares no answer_source_reason (SPEC 9.4.5)", name)
			continue
		}
		assertEnum(t, name+".answer_source_reason",
			reason, "generator_not_configured", "generator_unavailable", "generator_error")

		// Neither may be required: absent means generated, which is what keeps
		// the addition non-breaking for existing clients.
		for _, field := range []string{"answer_source", "answer_source_reason"} {
			if contains(requiredNames(t, tool, name), field) {
				t.Errorf("%s marks %s required; 9.4.5 makes it optional and absent means generated",
					name, field)
			}
		}
	}
	if seen == 0 {
		t.Fatal("none of the answer surfaces is advertised; this test asserted nothing")
	}
}

// TestAnswerProvenance_ThePairingIsEnforceableByAClient is the half a property
// check misses. 9.4.5 PAIRS the fields, and a client can only enforce a
// pairing the SERVED schema states: the canonical contract carrying it is no
// help to a caller validating what the server advertised.
//
// The relationships are decoded and asserted as relationships. Searching the
// serialized schema for the right words would accept
// `{"allOf":[{"description":"answer_source ... retrieval_only unchecked"}]}`,
// which constrains nothing at all.
func TestAnswerProvenance_ThePairingIsEnforceableByAClient(t *testing.T) {
	srv := requireServer(t, corpus)
	tools := dial(t, srv).tools(t)

	seen := 0
	for _, name := range answerSurfaces {
		tool, ok := tools[name]
		if !ok {
			continue
		}
		schema := outputSchema(t, tool, name)
		if schema == nil {
			continue
		}
		seen++
		riders := decodeRiders(t, schema["allOf"], name)
		if riders == nil {
			continue
		}

		// retrieval_only implies a reason, and implies faithfulness unchecked.
		forward := riderWhoseIfConstrains(t, riders, "answer_source", "retrieval_only", name)
		if forward == nil {
			t.Errorf("%s states no rider conditioned on answer_source=retrieval_only, so a "+
				"client cannot enforce that a retrieval-only answer names a reason", name)
		} else {
			if !riderThenRequires(t, forward, "answer_source_reason", name) {
				t.Errorf("%s: retrieval_only does not require answer_source_reason", name)
			}
			if got := riderThenConst(forward, "faithfulness"); got != "unchecked" {
				t.Errorf("%s: retrieval_only pins faithfulness to %q, want \"unchecked\" "+
					"(nothing was generated to verify)", name, got)
			}
		}

		// A reason implies retrieval_only.
		reverse := riderWhoseIfRequires(t, riders, "answer_source_reason", name)
		if reverse == nil {
			t.Errorf("%s states no rider conditioned on the presence of answer_source_reason", name)
			continue
		}
		if got := riderThenConst(reverse, "answer_source"); got != "retrieval_only" {
			t.Errorf("%s: a reason pins answer_source to %q, want \"retrieval_only\"", name, got)
		}
	}
	if seen == 0 {
		t.Fatal("no answer surface advertised an output schema; this test asserted nothing")
	}
}

// decodeRiders turns an advertised allOf into a list of objects.
func decodeRiders(t *testing.T, raw interface{}, label string) []map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(raw)
	if err != nil || string(encoded) == "null" {
		t.Errorf("%s output schema states no allOf", label)
		return nil
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(encoded, &list); err != nil {
		t.Errorf("%s allOf is not a list of objects: %v", label, err)
		return nil
	}
	if len(list) == 0 {
		t.Errorf("%s declares an empty allOf", label)
		return nil
	}
	return list
}

func subObject(node map[string]interface{}, path ...string) map[string]interface{} {
	current := node
	for _, key := range path {
		if current == nil {
			return nil
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// stringList reads a JSON Schema string array, reporting anything malformed.
//
// Dropping a non-string entry silently would let `required: ["x", 42]` read as
// `["x"]`, so an advertised schema that is not valid JSON Schema satisfies the
// assertions built on it.
func stringList(t *testing.T, node map[string]interface{}, key, label string) []string {
	t.Helper()
	raw, present := node[key]
	if !present {
		return nil
	}
	listed, ok := raw.([]interface{})
	if !ok {
		t.Errorf("%s: `%s` is %T, not an array", label, key, raw)
		return nil
	}
	out := make([]string, 0, len(listed))
	for i, entry := range listed {
		value, ok := entry.(string)
		if !ok {
			t.Errorf("%s: `%s`[%d] is %T, not a string: %v", label, key, i, entry, entry)
			continue
		}
		out = append(out, value)
	}
	return out
}

// riderWhoseIfConstrains finds the rider whose `if` pins field to value AND
// requires it to be present.
//
// `properties` alone constrains a key only when it is there, so an `if` of
// just `{"properties": {"answer_source": {"const": "retrieval_only"}}}` also
// matches a document with NO answer_source. Such a rider would force
// answer_source_reason onto every ordinary generated answer, which is the
// opposite of 9.4.5. The `required` is what makes the condition mean
// "answer_source = retrieval_only" rather than "absent or retrieval_only".
func riderWhoseIfConstrains(t *testing.T, riders []map[string]interface{}, field, value, label string) map[string]interface{} {
	t.Helper()
	for _, rider := range riders {
		prop := subObject(rider, "if", "properties", field)
		if prop == nil || prop["const"] != value {
			continue
		}
		cond := subObject(rider, "if")
		if !contains(stringList(t, cond, "required", label+" if"), field) {
			t.Errorf("%s: the rider pinning %s=%q does not require %s to be present, so it "+
				"also applies when %s is absent", label, field, value, field, field)
			continue
		}
		return rider
	}
	return nil
}

// riderWhoseIfRequires finds the rider conditioned on a field's PRESENCE.
// A rider that also pins a const is the other direction, so it is skipped.
func riderWhoseIfRequires(t *testing.T, riders []map[string]interface{}, field, label string) map[string]interface{} {
	t.Helper()
	for _, rider := range riders {
		cond := subObject(rider, "if")
		if cond == nil || subObject(cond, "properties", field) != nil {
			continue
		}
		for _, required := range stringList(t, cond, "required", label+" if") {
			if required == field {
				return rider
			}
		}
	}
	return nil
}

func riderThenRequires(t *testing.T, rider map[string]interface{}, field, label string) bool {
	t.Helper()
	then := subObject(rider, "then")
	if then == nil {
		return false
	}
	for _, required := range stringList(t, then, "required", label+" then") {
		if required == field {
			return true
		}
	}
	return false
}

// riderThenConst reads the value `then` pins field to, or "" when unpinned.
func riderThenConst(rider map[string]interface{}, field string) string {
	prop := subObject(rider, "then", "properties", field)
	if prop == nil {
		return ""
	}
	value, _ := prop["const"].(string)
	return value
}

// TestAnswerProvenance_AGeneratedAnswerCarriesNeitherField pins the
// non-breaking half on the wire. A read-only server has no generator, so this
// is skipped rather than asserted: see the note in doc.go about what a
// credential-free run cannot reach.
func TestAnswerProvenance_BehaviourNeedsACredentialedRun(t *testing.T) {
	requireServer(t, corpus)
	t.Skip("asserting that a FAILED generator reports retrieval_only needs a credentialed " +
		"server plus an induced provider failure; the schema contract above is what a " +
		"credential-free run can hold")
}
