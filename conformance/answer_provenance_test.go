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

// TestAnswerProvenance_ThePairingIsEnforceableByAClient is the half that a
// property check misses. 9.4.5 pairs the fields, and a client can only
// enforce a pairing the SERVED schema states: the canonical contract carrying
// it is no help to a caller validating what the server advertised.
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
		encoded, err := json.Marshal(schema["allOf"])
		if err != nil || string(encoded) == "null" {
			t.Errorf("%s output schema states no conditional; a client cannot enforce "+
				"that retrieval_only requires a reason (SPEC 9.4.5)", name)
			continue
		}
		for _, needle := range []string{
			"answer_source", "answer_source_reason", "retrieval_only", "unchecked",
		} {
			if !bytesContain(encoded, needle) {
				t.Errorf("%s conditional does not bind %q: %s", name, needle, encoded)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no answer surface advertised an output schema; this test asserted nothing")
	}
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

func bytesContain(haystack []byte, needle string) bool {
	return len(needle) > 0 && indexOf(haystack, []byte(needle)) >= 0
}

func indexOf(haystack, needle []byte) int {
outer:
	for i := 0; i+len(needle) <= len(haystack); i++ {
		for j := range needle {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
