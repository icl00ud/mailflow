package mail

import (
	"bytes"
	"fmt"
	htmpl "html/template"
	"os"
	"path/filepath"
	"strings"
	"sync"
	ttmpl "text/template"

	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/store"
)

// Data is what every email template receives.
type Data struct {
	Store     flow.StoreCfg
	Step      flow.Step
	Order     store.Order
	FirstName string
	Items     []store.Item
	Tracking  string
	Carrier   string
	TrackURL  string
	// WindowDays is the realistic upper bound of the journey, for copy that sets expectations.
	Subject string
}

type Renderer struct {
	mu    sync.Mutex
	cache map[string]*htmpl.Template
}

func NewRenderer() *Renderer { return &Renderer{cache: map[string]*htmpl.Template{}} }

var funcs = htmpl.FuncMap{"upper": strings.ToUpper}

// Render returns subject, html and text for a step. It is strict: a template
// referencing a missing field fails here instead of emailing "<no value>".
func (r *Renderer) Render(f *flow.Flow, step flow.Step, d Data) (subject, htmlBody, text string, err error) {
	st, err := ttmpl.New("subject").Option("missingkey=error").Parse(step.Subject)
	if err != nil {
		return "", "", "", fmt.Errorf("subject: %w", err)
	}
	var sb bytes.Buffer
	if err := st.Execute(&sb, d); err != nil {
		return "", "", "", fmt.Errorf("subject: %w", err)
	}
	subject = strings.TrimSpace(sb.String())
	d.Subject = subject

	t, err := r.load(f, step.Template)
	if err != nil {
		return "", "", "", err
	}
	var hb bytes.Buffer
	if err := t.ExecuteTemplate(&hb, "layout", d); err != nil {
		return "", "", "", fmt.Errorf("template %s: %w", step.Template, err)
	}
	htmlBody = hb.String()
	if strings.Contains(htmlBody, "<no value>") {
		return "", "", "", fmt.Errorf("template %s: rendered <no value>", step.Template)
	}
	return subject, htmlBody, TextFromHTML(htmlBody), nil
}

func (r *Renderer) load(f *flow.Flow, name string) (*htmpl.Template, error) {
	key := f.Store.ID + "/" + name
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.cache[key]; ok {
		return t, nil
	}
	dir := filepath.Join(f.Dir, "templates")
	t, err := htmpl.New("root").Funcs(funcs).Option("missingkey=error").
		ParseFiles(filepath.Join(dir, "_layout.html"), filepath.Join(dir, name+".html"))
	if err != nil {
		return nil, err
	}
	r.cache[key] = t
	return t, nil
}

// Preload parses every active template so a typo fails at boot, not at 3am.
func (r *Renderer) Preload(f *flow.Flow) error {
	for _, s := range f.Steps {
		if s.Status == flow.StatusDraft {
			continue
		}
		if _, err := r.load(f, s.Template); err != nil {
			return fmt.Errorf("%s step %s: %w", f.Store.ID, s.ID, err)
		}
	}
	_, err := os.Stat(filepath.Join(f.Dir, "templates", "_layout.html"))
	return err
}
