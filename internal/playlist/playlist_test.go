package playlist

import (
	"math/rand/v2"
	"strings"
	"testing"
)

const sample = `{
  "schema_version": 1,
  "entries": [
    {"id": "interference-fiesta", "map": {"asset_id": "70f884d7-6869-469d-b4d2-4219627e2d83", "version_id": "cc791b4b-054a-4653-9034-5dc13c809c54"},
     "mode": {"asset_id": "aca7bbf8-7a18-4aae-8785-1bd3f58275fd", "version_id": "3685f6b2-2860-4e98-9d13-513087edb465"}},
    {"id": "kusini-ctf", "map": {"asset_id": "4eb7a3ac-81f7-4faa-acd8-ce6bbba667af", "version_id": "98a5391c-4a3a-4f04-bdc7-6db58cc27433"},
     "mode": {"asset_id": "8650f7e0-1f82-4d45-a127-32dd54df06e5", "version_id": "2cb07a58-190d-4fbc-a071-721147faa9e4"}},
    {"id": "bazaar-slayer", "map": {"asset_id": "298d5036-cd43-47b3-a4bd-31e127566593", "version_id": "5546a6ec-841d-4955-be7a-5f32c3ac0428"},
     "mode": {"asset_id": "1e8cd10b-1496-423b-8699-f98f6f5db67e", "version_id": "ae66f4aa-a6fc-464a-ae9f-e2db13c1342d"}},
    {"id": "off", "enabled": false, "map": {"asset_id": "298d5036-cd43-47b3-a4bd-31e127566593", "version_id": "5546a6ec-841d-4955-be7a-5f32c3ac0428"},
     "mode": {"asset_id": "1e8cd10b-1496-423b-8699-f98f6f5db67e", "version_id": "ae66f4aa-a6fc-464a-ae9f-e2db13c1342d"}}
  ]
}`

func TestParseDefaultsAndDisabled(t *testing.T) {
	f, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Selection != "shuffle_bag" || len(f.Entries) != 3 {
		t.Fatalf("got selection %q entries %d", f.Selection, len(f.Entries))
	}
	for _, e := range f.Entries {
		if e.ID == "off" {
			t.Fatalf("bad entry %+v", e)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for name, doc := range map[string]string{
		"schema":    `{"schema_version": 2, "entries": []}`,
		"selection": strings.Replace(sample, `"schema_version": 1,`, `"schema_version": 1, "selection": "weighted",`, 1),
		"duplicate": strings.Replace(sample, `"kusini-ctf"`, `"interference-fiesta"`, 1),
		"uuid":      strings.Replace(sample, "70f884d7-6869-469d-b4d2-4219627e2d83", "70f884d7", 1),
		"zero":      strings.Replace(sample, "70f884d7-6869-469d-b4d2-4219627e2d83", "00000000-0000-0000-0000-000000000000", 1),
		"noid":      strings.Replace(sample, `"id": "kusini-ctf",`, ``, 1),
		"empty":     `{"schema_version": 1, "entries": []}`,
		"json":      `{`,
		"longid":    strings.Replace(sample, `"kusini-ctf"`, `"`+strings.Repeat("k", 81)+`"`, 1),
		// 27 three-byte characters = 81 bytes: the limit counts bytes, not characters.
		"longname": strings.Replace(sample, `{"id": "kusini-ctf",`, `{"id": "kusini-ctf", "name": "`+strings.Repeat("界", 27)+`",`, 1),
		"control":  strings.Replace(sample, `{"id": "kusini-ctf",`, `{"id": "kusini-ctf", "name": "CTF\non Kusini",`, 1), // a JSON \n escape
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseAcceptsLimits(t *testing.T) {
	doc := strings.Replace(sample, `"kusini-ctf"`, `"`+strings.Repeat("k", 80)+`"`, 1)
	doc = strings.Replace(doc, `{"id": "bazaar-slayer",`, `{"id": "bazaar-slayer", "name": "`+strings.Repeat("é", 40)+`",`, 1)
	if _, err := Parse([]byte(doc)); err != nil {
		t.Fatalf("80-byte id and name rejected: %v", err)
	}
}

func TestShuffleBagCyclesWithoutBoundaryRepeat(t *testing.T) {
	f, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBag(f, rand.New(rand.NewPCG(1, 2)))
	prev := ""
	for cycle := 0; cycle < 200; cycle++ {
		seen := map[string]bool{}
		for range f.Entries {
			e := b.Next()
			if seen[e.ID] {
				t.Fatalf("cycle %d repeated %s", cycle, e.ID)
			}
			if e.ID == prev {
				t.Fatalf("cycle %d immediate repeat %s", cycle, e.ID)
			}
			seen[e.ID], prev = true, e.ID
		}
	}
}

func TestSequentialAndSingleEntry(t *testing.T) {
	f, _ := Parse([]byte(strings.Replace(sample, `"schema_version": 1,`, `"schema_version": 1, "selection": "sequential",`, 1)))
	b := NewBag(f, nil)
	for i := 0; i < 7; i++ {
		if got, want := b.Next().ID, f.Entries[i%3].ID; got != want {
			t.Fatalf("step %d got %s want %s", i, got, want)
		}
	}
	one := &File{Selection: "shuffle_bag", Entries: f.Entries[:1]}
	b = NewBag(one, nil)
	for i := 0; i < 3; i++ {
		if b.Next().ID != one.Entries[0].ID {
			t.Fatal("single entry")
		}
	}
}

// The playlist shipped in the host release zip must stay valid.
func TestPackagedExample(t *testing.T) {
	f, err := Load("../../packaging/host/playlist.example.json")
	if err != nil || len(f.Entries) == 0 {
		t.Fatalf("packaged playlist example: %v", err)
	}
}

func TestThumbRef(t *testing.T) {
	const asset, version = "70f884d7-6869-469d-b4d2-4219627e2d83", "cc791b4b-054a-4653-9034-5dc13c809c54"
	f, err := Parse([]byte(`{"schema_version":1,"entries":[{"id":"a","map":{"asset_id":"` + asset + `","version_id":"` + version +
		`"},"mode":{"asset_id":"` + asset + `","version_id":"` + version + `"}}]}`))
	if err != nil || f.Entries[0].ThumbRef() != asset+"/"+version {
		t.Fatalf("ref %q %v", f.Entries[0].ThumbRef(), err)
	}
}

func TestParseTeams(t *testing.T) {
	with := func(teams string) string {
		return strings.Replace(sample, `{"id": "kusini-ctf",`, `{"id": "kusini-ctf", "teams": `+teams+`,`, 1)
	}
	for _, bad := range []string{`{}`, `{"count": 9}`, `{"count": -1}`, `{"count": 1}`, `{"size": 33}`, `{"size": -2}`, `{"size": 1}`} {
		if _, err := Parse([]byte(with(bad))); err == nil {
			t.Errorf("teams %s accepted", bad)
		}
	}
	for teams, want := range map[string]Teams{`{"count": 4}`: {Count: 4}, `{"size": 3}`: {Size: 3}, `{"count": 8, "size": 32}`: {Count: 8, Size: 32}} {
		f, err := Parse([]byte(with(teams)))
		if err != nil {
			t.Fatalf("teams %s rejected: %v", teams, err)
		}
		var got *Teams
		for _, e := range f.Entries {
			if e.ID == "kusini-ctf" {
				got = e.Teams
			}
		}
		if got == nil || *got != want {
			t.Errorf("teams %s parsed as %+v", teams, got)
		}
	}
}
