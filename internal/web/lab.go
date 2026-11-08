package web

// The regression lab library: saved cases and suites that live beside the
// recordings they describe, exactly where `tapelog lab run --dir <dir>` looks.
// The workbench therefore gains a small, tightly bounded write capability, and
// the rules for it are deliberately strict:
//
//   - only plain *.json leaf names inside the recordings directory,
//   - nothing that already exists is replaced unless the caller proves it read
//     the current revision,
//   - nothing is ever written outside that directory, and no recording is
//     touched.
//
// Analysis is delegated to internal/lab. The browser never decides PASS, FAIL or
// INCONCLUSIVE; it renders what the runner reports.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/lab"
)

const (
	maxLabFiles = 500
	// maxSuiteComparisons bounds how many reference comparisons one suite run
	// performs, because each reads and analyses two recordings.
	maxSuiteComparisons = 20
)

// labCaseEntry is one saved case as the library lists it.
type labCaseEntry struct {
	File            string            `json:"file"`
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	Evidence        string            `json:"evidence"`
	Eligibility     string            `json:"eligibility,omitempty"`
	EvidenceSeqs    []uint64          `json:"evidence_seqs,omitempty"`
	EvidencePresent bool              `json:"evidence_present"`
	Reference       string            `json:"reference,omitempty"`
	Expectations    []lab.Expectation `json:"expectations"`
	Revision        string            `json:"revision"`
	Invalid         string            `json:"invalid,omitempty"`
}

// labSuiteEntry is one saved suite as the library lists it.
type labSuiteEntry struct {
	File     string   `json:"file"`
	Title    string   `json:"title"`
	Cases    []string `json:"cases"`
	Missing  []string `json:"missing,omitempty"`
	Revision string   `json:"revision"`
	Invalid  string   `json:"invalid,omitempty"`
}

type labIndex struct {
	Directory string          `json:"directory"`
	Cases     []labCaseEntry  `json:"cases"`
	Suites    []labSuiteEntry `json:"suites"`
	Notes     []string        `json:"notes"`
	Limits    map[string]int  `json:"limits"`
}

// labDocKind classifies one JSON document in the evidence directory.
type labDocKind int

const (
	labDocUnknown labDocKind = iota
	labDocCase
	labDocSuite
)

// classifyLabDoc decides whether a file is a case, a suite, or something else
// that merely happens to be JSON. Unrelated JSON is skipped rather than reported
// as a broken case; a document that claims to be a case but fails validation is
// reported with its error.
func classifyLabDoc(data []byte) (labDocKind, lab.Case, lab.Suite, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return labDocUnknown, lab.Case{}, lab.Suite{}, nil
	}
	_, hasCases := probe["cases"]
	_, hasExpectations := probe["expectations"]
	switch {
	case hasCases:
		s, err := lab.ParseSuite(data)
		if err != nil {
			return labDocSuite, lab.Case{}, lab.Suite{}, err
		}
		return labDocSuite, lab.Case{}, s, nil
	case hasExpectations:
		c, err := lab.ParseCase(data)
		if err != nil {
			return labDocCase, lab.Case{}, lab.Suite{}, err
		}
		return labDocCase, c, lab.Suite{}, nil
	default:
		return labDocUnknown, lab.Case{}, lab.Suite{}, nil
	}
}

// revisionOf is the content identity the interface quotes back when it wants to
// replace a file it has already read.
func revisionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// labLeaf reports whether name is a plain *.json filename.
func labLeaf(name string) bool {
	return leafName(name, ".json")
}

// leafName reports whether name is a plain filename with the wanted suffix. It
// is used for both lab documents (*.json) and the recordings they name (*.jsonl),
// so the two never disagree about what "inside the directory" means.
func leafName(name, ext string) bool {
	return name != "" && name == strings.TrimSpace(name) && !strings.ContainsAny(name, "/\\:\x00\r\n") &&
		name != "." && name != ".." && !strings.Contains(name, "..") &&
		strings.HasSuffix(strings.ToLower(name), ext)
}

// labDirectory returns the recordings directory the lab operates on.
func (s *server) labDirectory() (string, error) {
	if s.dir == "" {
		return "", fmt.Errorf("the regression lab needs directory mode: start tapelog web --dir <recordings directory>")
	}
	return s.dir, nil
}

// labFileExists reports whether a plain leaf file exists in the directory,
// without following symlinks.
func labFileExists(dir, name string) bool {
	if !leafName(name, ".json") && !leafName(name, ".jsonl") {
		return false
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	st, err := root.Lstat(name)
	return err == nil && st.Mode().IsRegular()
}

// readLabDoc reads one case or suite document with the lab's own limits.
func readLabDoc(dir, name string) ([]byte, string, error) {
	data, err := lab.ReadLeaf(dir, name, ".json", lab.MaxDefinition)
	if err != nil {
		return nil, "", err
	}
	return data, revisionOf(data), nil
}

// writeLabDoc creates a lab document, or replaces one the caller has read.
// replaces must be the revision of the file as it exists on disk; an empty
// replaces means "create only". Anything else is refused, so a case can never be
// silently overwritten by a stale tab.
func writeLabDoc(dir, name string, data []byte, replaces string) error {
	if !labLeaf(name) {
		return fmt.Errorf("a lab file must be a plain .json filename")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	current := ""
	if st, statErr := root.Lstat(name); statErr == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", name)
		}
		existing, readErr := lab.ReadLeaf(dir, name, ".json", lab.MaxDefinition)
		if readErr != nil {
			return fmt.Errorf("%s exists and cannot be read: %v", name, readErr)
		}
		current = revisionOf(existing)
	}
	switch {
	case current == "" && replaces != "":
		return fmt.Errorf("%s no longer exists; save it again as a new case", name)
	case current != "" && replaces == "":
		return fmt.Errorf("%s already exists; confirm the replacement or choose another name", name)
	case current != "" && replaces != current:
		return fmt.Errorf("%s changed on disk since you opened it; reload it before saving", name)
	}
	tmp := name + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		root.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		root.Remove(tmp)
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		root.Remove(tmp)
		return err
	}
	return nil
}

// caseEntry turns a parsed case into a library row.
func caseEntry(dir, name string, c lab.Case, rev string) labCaseEntry {
	return labCaseEntry{
		File: name, Title: c.Title, Description: c.Description, Evidence: c.Evidence,
		EvidencePresent: labFileExists(dir, c.Evidence), Reference: c.Reference,
		Expectations: c.Expectations, Revision: rev,
		Eligibility: c.Eligibility, EvidenceSeqs: c.EvidenceSeqs,
	}
}

// labIndexOf lists the saved cases and suites in the directory. One bounded
// directory scan, the same read the session library already performs.
func labIndexOf(dir string) (*labIndex, error) {
	index := &labIndex{
		Directory: dir,
		Cases:     []labCaseEntry{},
		Suites:    []labSuiteEntry{},
		Notes:     []string{},
		Limits: map[string]int{
			"files": maxLabFiles, "definition_bytes": lab.MaxDefinition,
			"expectations": lab.MaxChecks, "suite_cases": lab.MaxCases,
			"recording_bytes": lab.MaxRecording,
		},
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(maxLabFiles + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > maxLabFiles {
		index.Notes = append(index.Notes, fmt.Sprintf("Only the first %d directory entries were examined.", maxLabFiles))
		entries = entries[:maxLabFiles]
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !labLeaf(name) {
			continue
		}
		data, rev, err := readLabDoc(dir, name)
		if err != nil {
			index.Notes = append(index.Notes, fmt.Sprintf("%s could not be read: %v", name, err))
			continue
		}
		kind, c, suite, parseErr := classifyLabDoc(data)
		switch kind {
		case labDocCase:
			entry := caseEntry(dir, name, c, rev)
			if parseErr != nil {
				entry.Invalid = parseErr.Error()
			}
			index.Cases = append(index.Cases, entry)
		case labDocSuite:
			entry := labSuiteEntry{File: name, Title: suite.Title, Cases: suite.Cases, Revision: rev}
			if parseErr != nil {
				entry.Invalid = parseErr.Error()
			}
			for _, ref := range entry.Cases {
				if !labFileExists(dir, ref) {
					entry.Missing = append(entry.Missing, ref)
				}
			}
			index.Suites = append(index.Suites, entry)
		}
	}
	sort.Slice(index.Cases, func(i, j int) bool {
		return strings.ToLower(index.Cases[i].Title) < strings.ToLower(index.Cases[j].Title)
	})
	sort.Slice(index.Suites, func(i, j int) bool {
		return strings.ToLower(index.Suites[i].Title) < strings.ToLower(index.Suites[j].Title)
	})
	if len(index.Cases) == 0 && len(index.Suites) == 0 {
		index.Notes = append(index.Notes, "No saved cases yet. Open a recording, select a call and save a case from the Make a test tab.")
	}
	return index, nil
}

// labIndexAPI serves the saved case and suite library.
func (s *server) labIndexAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	index, err := labIndexOf(dir)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, index)
}

// labCaseAPI serves one saved case with its raw definition and evidence state,
// so the interface can open, edit and re-run it.
func (s *server) labCaseAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	name := r.URL.Query().Get("file")
	data, rev, err := readLabDoc(dir, name)
	if err != nil {
		apiError(w, 400, fmt.Errorf("the case %q cannot be opened", name))
		return
	}
	kind, c, _, err := classifyLabDoc(data)
	if kind != labDocCase {
		apiError(w, 422, fmt.Errorf("%q is not a regression case", name))
		return
	}
	evidence := map[string]any{"file": c.Evidence, "present": labFileExists(dir, c.Evidence)}
	if rec, recErr := s.recording(c.Evidence); recErr == nil {
		evidence["revision"] = rec.Info.Revision
		evidence["matches_saved"] = c.EvidenceSHA256 == "" || c.EvidenceSHA256 == rec.Info.Revision
		evidence["complete"] = rec.Info.Complete
		evidence["chain_ok"] = rec.Info.ChainOK
	}
	resp := map[string]any{
		"case": caseEntry(dir, name, c, rev), "json": string(data), "evidence": evidence,
		"valid": err == nil,
	}
	if err != nil {
		resp["invalid"] = err.Error()
	}
	writeJSON(w, resp)
}

// labSaveCaseAPI creates a saved case, or replaces one the caller has read.
func (s *server) labSaveCaseAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	var req struct {
		File     string          `json:"file"`
		Case     json.RawMessage `json:"case"`
		Replaces string          `json:"replaces"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, lab.MaxDefinition))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apiError(w, 400, fmt.Errorf("invalid request: %v", err))
		return
	}
	c, err := lab.ParseCase(req.Case)
	if err != nil {
		apiError(w, 422, fmt.Errorf("this case is not valid: %v", err))
		return
	}
	if req.Replaces != "" && !labLeaf(req.File) {
		apiError(w, 400, fmt.Errorf("invalid case filename"))
		return
	}
	name := req.File
	if name == "" {
		name = labSuggestedName(c.Title)
	}
	if !labLeaf(name) {
		apiError(w, 400, fmt.Errorf("%q is not a usable case filename; use letters, digits and dashes", name))
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		apiError(w, 500, err)
		return
	}
	if err := writeLabDoc(dir, name, append(data, '\n'), req.Replaces); err != nil {
		status := 422
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "changed on disk") {
			status = 409
		}
		apiError(w, status, err)
		return
	}
	saved, rev, err := readLabDoc(dir, name)
	if err != nil {
		apiError(w, 500, err)
		return
	}
	_, stored, _, _ := classifyLabDoc(saved)
	writeJSON(w, map[string]any{
		"file": name, "revision": rev, "case": caseEntry(dir, name, stored, rev),
		"command": "tapelog lab run --dir . --case " + name,
	})
}

// labSaveSuiteAPI creates or replaces a suite definition.
func (s *server) labSaveSuiteAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	var req struct {
		File     string          `json:"file"`
		Suite    json.RawMessage `json:"suite"`
		Replaces string          `json:"replaces"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, lab.MaxDefinition))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apiError(w, 400, fmt.Errorf("invalid request: %v", err))
		return
	}
	suite, err := lab.ParseSuite(req.Suite)
	if err != nil {
		apiError(w, 422, fmt.Errorf("this suite is not valid: %v", err))
		return
	}
	name := req.File
	if name == "" {
		name = labSuggestedName(suite.Title)
	}
	if !labLeaf(name) {
		apiError(w, 400, fmt.Errorf("%q is not a usable suite filename", name))
		return
	}
	data, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		apiError(w, 500, err)
		return
	}
	if err := writeLabDoc(dir, name, append(data, '\n'), req.Replaces); err != nil {
		status := 422
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "changed on disk") {
			status = 409
		}
		apiError(w, status, err)
		return
	}
	missing := []string{}
	for _, ref := range suite.Cases {
		if !labFileExists(dir, ref) {
			missing = append(missing, ref)
		}
	}
	_, rev, _ := readLabDoc(dir, name)
	writeJSON(w, map[string]any{
		"file": name, "revision": rev, "missing": missing,
		"command": "tapelog lab suite --dir . --suite " + name,
	})
}

// labSuggestedName derives a filename from a title: lower case, dashes, bounded.
func labSuggestedName(title string) string {
	var b strings.Builder
	last := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			last = false
		case !last && b.Len() > 0:
			b.WriteByte('-')
			last = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "case.json"
	}
	return out + ".json"
}

// labCompare reports an observational comparison between two recordings and can
// never turn a difference into a failure.
func labCompare(dir, reference, candidate string) (any, string) {
	if reference == "" || candidate == "" || reference == candidate {
		return nil, "not_requested"
	}
	delta, err := BehaviourWorkflowCompare(dir, reference, candidate)
	if err != nil {
		return nil, "unavailable"
	}
	return delta.Result, delta.Status
}

// labRunAPI runs one saved case against the evidence it names, or against an
// explicitly chosen candidate recording.
func (s *server) labRunAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	var req struct {
		File      string `json:"file"`
		Candidate string `json:"candidate"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apiError(w, 400, fmt.Errorf("invalid request: %v", err))
		return
	}
	data, rev, err := readLabDoc(dir, req.File)
	if err != nil {
		apiError(w, 400, fmt.Errorf("the case %q cannot be opened", req.File))
		return
	}
	kind, c, _, perr := classifyLabDoc(data)
	if kind != labDocCase {
		apiError(w, 422, fmt.Errorf("%q is not a regression case", req.File))
		return
	}
	if perr != nil {
		apiError(w, 422, fmt.Errorf("%q is not a valid case: %v", req.File, perr))
		return
	}
	result, err := lab.RunCase(dir, c, req.Candidate)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	comparison, comparisonStatus := labCompare(dir, c.Reference, result.Evidence)
	resp := map[string]any{
		"file": req.File, "revision": rev, "case": caseEntry(dir, req.File, c, rev),
		"result": result, "comparison": comparison, "comparison_status": comparisonStatus,
		"command": "tapelog lab run --dir . --case " + req.File,
	}
	if req.Candidate != "" {
		resp["command"] = "tapelog lab run --dir . --case " + req.File + " --candidate " + req.Candidate
	}
	writeJSON(w, resp)
}

// labSuiteRunAPI runs a saved suite with the offline runner, and attaches an
// observational comparison to each case that names a reference recording.
func (s *server) labSuiteRunAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	dir, err := s.labDirectory()
	if err != nil {
		apiError(w, 400, err)
		return
	}
	var req struct {
		File      string `json:"file"`
		Candidate string `json:"candidate"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apiError(w, 400, fmt.Errorf("invalid request: %v", err))
		return
	}
	data, rev, err := readLabDoc(dir, req.File)
	if err != nil {
		apiError(w, 400, fmt.Errorf("the suite %q cannot be opened", req.File))
		return
	}
	kind, _, suite, perr := classifyLabDoc(data)
	if kind != labDocSuite {
		apiError(w, 422, fmt.Errorf("%q is not a regression suite", req.File))
		return
	}
	if perr != nil {
		apiError(w, 422, fmt.Errorf("%q is not a valid suite: %v", req.File, perr))
		return
	}
	result, err := lab.RunSuiteCandidate(dir, suite, req.Candidate)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	comparisons := []map[string]any{}
	for _, name := range suite.Cases {
		if len(comparisons) >= maxSuiteComparisons {
			break
		}
		raw, _, err := readLabDoc(dir, name)
		if err != nil {
			continue
		}
		kind, c, _, err := classifyLabDoc(raw)
		if kind != labDocCase || err != nil || c.Reference == "" {
			continue
		}
		candidate := c.Evidence
		if req.Candidate != "" {
			candidate = req.Candidate
		}
		comparison, status := labCompare(dir, c.Reference, candidate)
		if comparison == nil {
			continue
		}
		comparisons = append(comparisons, map[string]any{
			"file": name, "title": c.Title, "candidate": candidate,
			"reference": c.Reference, "status": status, "comparison": comparison,
		})
	}
	writeJSON(w, map[string]any{
		"file": req.File, "revision": rev, "suite": result, "comparisons": comparisons,
		"case_files":            suite.Cases,
		"comparisons_truncated": len(comparisons) == maxSuiteComparisons && len(suite.Cases) > maxSuiteComparisons,
		"command":               "tapelog lab suite --dir . --suite " + req.File,
	})
}
