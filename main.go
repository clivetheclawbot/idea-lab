// Command idea-lab is a tiny local web app for running visual idea sessions.
//
// It serves a gallery of "boards": each board is a title, a short subtitle,
// a handful of bullets, and an illustration generated with the OpenAI images
// API (gpt-image-1). Real-world photo references come from the Openverse API.
//
// Run the server:
//
//	./idea-lab -addr 0.0.0.0:8899
//
// Create a board from the command line (same binary, client mode):
//
//	./idea-lab -new \
//	  -title "Cellar Tracker" \
//	  -subtitle "Drink what you own, before it peaks" \
//	  -bullets "Scan labels;Drinking-window nudges;Pairs with dinner" \
//	  -prompt "A cozy minimalist wine cellar app concept, warm pastel illustration"
//
// API:
//
//	POST /api/board   {"title","subtitle","bullets":[],"imagePrompt","imageUrl"}
//	GET  /api/boards  -> JSON list of all boards
//	GET  /api/photo?q=cellar -> Openverse photo references (title, url, thumb)
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const dir = "."

// Board is one idea card shown in the gallery.
type Board struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle"`
	Bullets     []string  `json:"bullets"`
	ImagePrompt string    `json:"imagePrompt"`
	ImagePath   string    `json:"imagePath"` // served path, e.g. /img/x.png; empty = gradient placeholder
	ImageURL    string    `json:"imageUrl"`  // remote photo, used if provided directly
	CreatedAt   time.Time `json:"created_at"`
	ImageNote   string    `json:"image_note,omitempty"` // error/notice shown under the image area
}

func boardsDir() string { return filepath.Join(dir, "boards") }
func imgDir() string    { return filepath.Join(dir, "img") }

func loadBoards() []Board {
	entries, err := os.ReadDir(boardsDir())
	if err != nil {
		return nil
	}
	var boards []Board
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(boardsDir(), e.Name()))
		if err != nil {
			continue
		}
		var b Board
		if json.Unmarshal(raw, &b) == nil {
			boards = append(boards, b)
		}
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].CreatedAt.After(boards[j].CreatedAt) })
	return boards
}

func saveBoard(b Board) error {
	if err := os.MkdirAll(boardsDir(), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(boardsDir(), b.ID+".json"), raw, 0o644)
}

// slugID makes a readable, collision-resistant id like "0311-cellar-tracker".
func slugID(title string) string {
	t := time.Now()
	slug := strings.ToLower(strings.Join(strings.Fields(title), "-"))
	slug = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return -1
		}
	}, slug)
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return fmt.Sprintf("%s-%s", t.Format("0102-1504"), slug)
}

// generateImage calls the OpenAI images API and stores the PNG locally.
// It returns the served path and, on failure, an empty path plus a note.
func generateImage(prompt string) (string, string) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		data, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "OPENAI_API_KEY="); ok {
					key = strings.TrimSpace(v)
				}
			}
		}
	}
	if key == "" {
		return "", "no OPENAI_API_KEY found — board uses a placeholder"
	}

	body, _ := json.Marshal(map[string]any{
		"model":  "gpt-image-1",
		"prompt": "Clean modern editorial illustration, soft pastel palette, gentle texture, uncluttered composition. Scene: " + prompt,
		"size":   "1024x1024",
		"n":      1,
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return "", "bad request build"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "image API unreachable: " + err.Error()
	}
	defer resp.Body.Close()

	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "image API response unreadable"
	}
	if resp.StatusCode != http.StatusOK || len(out.Data) == 0 {
		msg := out.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", "image generation failed: " + msg
	}

	raw, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil {
		return "", "image decode failed"
	}
	if err := os.MkdirAll(imgDir(), 0o755); err != nil {
		return "", "cannot write image dir"
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	path := filepath.Join(imgDir(), id+".png")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", "image write failed"
	}
	return "/img/" + id + ".png", ""
}

// --- Openverse photo reference search ---

type photoRef struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Thumb string `json:"thumb"`
}

func searchPhotos(query string) []photoRef {
	q := url.Values{"q": {query}, "page_size": {"8"}}
	req, err := http.NewRequest(http.MethodGet, "https://api.openverse.org/v1/images/?"+q.Encode(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "idea-lab/0.1 (local household tool)")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out struct {
		Results []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Thumbnail string `json:"thumbnail"`
		} `json:"results"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	refs := make([]photoRef, 0, len(out.Results))
	for _, r := range out.Results {
		refs = append(refs, photoRef{Title: r.Title, URL: r.URL, Thumb: r.Thumbnail})
	}
	return refs
}

// --- templates ---

const baseCSS = `
:root { --felt:#1e4d3b; --cream:#faf6ef; --ink:#26332c; --accent:#d9848b; }
* { box-sizing:border-box; margin:0; }
body { font-family:Georgia,'Times New Roman',serif; background:var(--cream); color:var(--ink); line-height:1.55; }
a { color:var(--felt); }
header { background:var(--felt); color:var(--cream); padding:1.1rem 2rem; display:flex; justify-content:space-between; align-items:baseline; }
header h1 { font-size:1.25rem; font-weight:normal; letter-spacing:.5px; }
header small { opacity:.75; }
main { max-width:960px; margin:2rem auto; padding:0 1.25rem; }
.board { background:#fff; border:1px solid #e5ddcd; border-radius:14px; overflow:hidden; margin-bottom:2rem; box-shadow:0 2px 14px rgba(30,77,59,.08); }
.board img.hero { width:100%; height:320px; object-fit:cover; display:block; }
.board .placeholder { width:100%; height:220px; background:linear-gradient(135deg,#dfe8df,#f2e6d8,#e8d5da); }
.board .body { padding:1.4rem 1.7rem 1.6rem; }
.board h2 { font-size:1.7rem; color:var(--felt); margin-bottom:.15rem; }
.board .sub { font-style:italic; opacity:.8; margin-bottom:.9rem; }
.board ul { list-style:none; }
.board li { padding:.28rem 0 .28rem 1.5rem; position:relative; }
.board li::before { content:"◆"; position:absolute; left:0; color:var(--accent); font-size:.7rem; top:.55rem; }
.note { font-size:.85rem; opacity:.65; margin-top:.8rem; }
.grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(280px,1fr)); gap:1.4rem; }
.card { background:#fff; border:1px solid #e5ddcd; border-radius:12px; overflow:hidden; text-decoration:none; color:inherit; transition:transform .12s; }
.card:hover { transform:translateY(-3px); }
.card img { width:100%; height:170px; object-fit:cover; display:block; }
.card .cbody { padding:.9rem 1.1rem; }
.card h3 { color:var(--felt); font-size:1.15rem; }
.card p { font-size:.92rem; opacity:.75; font-style:italic; }
.photos { display:flex; flex-wrap:wrap; gap:.8rem; }
.photos a { display:block; }
.photos img { width:200px; height:140px; object-fit:cover; border-radius:8px; border:1px solid #e5ddcd; }
footer { text-align:center; font-size:.8rem; opacity:.55; padding:2rem 0 3rem; }
`

const indexTmpl = `<!doctype html><html><head><meta charset="utf-8"><title>Idea Lab — gallery</title>
<meta name="viewport" content="width=device-width,initial-scale=1"><style>` + baseCSS + `</style></head><body>
<header><h1>Bertie's Idea Lab</h1><small>boards appear here as we make them</small></header><main>
{{if .Boards}}<div class="grid">
{{range .Boards}}<a class="card" href="/board/{{.ID}}">
{{if .ImagePath}}<img src="{{.ImagePath}}" alt="">{{else if .ImageURL}}<img src="{{.ImageURL}}" alt="">{{else}}<div class="placeholder" style="height:170px"></div>{{end}}
<div class="cbody"><h3>{{.Title}}</h3><p>{{.Subtitle}}</p></div></a>{{end}}
</div>{{else}}<p style="opacity:.6">No boards yet. Make one with:</p>
<pre style="background:#fff;padding:1rem;border-radius:8px;overflow:auto">./idea-lab -new -title "Cellar Tracker" -subtitle "…" -bullets "a;b;c" -prompt "…"</pre>{{end}}
</main><footer>edele-idea-lab · clive-box · <span id="clock">refresh for new boards</span></footer></body></html>`

const boardTmpl = `<!doctype html><html><head><meta charset="utf-8"><title>{{.Title}} — Idea Lab</title>
<meta name="viewport" content="width=device-width,initial-scale=1"><style>` + baseCSS + `</style></head><body>
<header><h1><a href="/" style="color:inherit;text-decoration:none">← Idea Lab</a></h1><small>{{.CreatedAt.Format "Mon 15:04"}}</small></header><main>
<div class="board">
{{if .ImagePath}}<img class="hero" src="{{.ImagePath}}" alt="">{{else if .ImageURL}}<img class="hero" src="{{.ImageURL}}" alt="">{{else}}<div class="placeholder"></div>{{end}}
<div class="body"><h2>{{.Title}}</h2>{{if .Subtitle}}<p class="sub">{{.Subtitle}}</p>{{end}}
{{if .Bullets}}<ul>{{range .Bullets}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .ImageNote}}<p class="note">{{.ImageNote}}</p>{{end}}</div></div>
</main><footer>edele-idea-lab</footer></body></html>`

var (
	tIndex = template.Must(template.New("i").Parse(indexTmpl))
	tBoard = template.Must(template.New("b").Parse(boardTmpl))
)

func idxPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tIndex.Execute(w, struct{ Boards []Board }{loadBoards()})
}

func boardPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/board/")
	for _, b := range loadBoards() {
		if b.ID == id {
			tBoard.Execute(w, b)
			return
		}
	}
	http.NotFound(w, r)
}

// apiBoard handles POST /api/board ({"title","subtitle","bullets":[],"imagePrompt","imageUrl"}).
func apiBoard(w http.ResponseWriter, r *http.Request) {
	var in Board
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	if in.ImageURL == "" && in.ImagePrompt != "" {
		path, note := generateImage(in.ImagePrompt)
		in.ImagePath, in.ImageNote = path, note
	}
	in.ID = slugID(in.Title)
	in.CreatedAt = time.Now()
	if err := saveBoard(in); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": "/board/" + in.ID, "id": in.ID})
}

// apiBoardEdit handles PUT /api/board/{id} — updates mutable fields.
// If imagePath is empty and imagePrompt is set, regenerates the illustration;
// otherwise keeps the existing image (fast content edits, no re-gen).
func apiBoardEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	boards := loadBoards()
	var existing *Board
	for i := range boards {
		if boards[i].ID == id {
			existing = &boards[i]
			break
		}
	}
	if existing == nil {
		http.Error(w, "no such board", http.StatusNotFound)
		return
	}
	var in Board
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if in.ImagePath == "" && in.ImagePrompt != "" && in.ImagePrompt != existing.ImagePrompt {
		path, note := generateImage(in.ImagePrompt)
		if path != "" {
			in.ImagePath = path
			in.ImageNote = ""
		} else {
			in.ImagePath, in.ImageNote = existing.ImagePath, note
		}
	} else if in.ImagePath == "" {
		in.ImagePath = existing.ImagePath
	}
	in.ID = existing.ID // preserve full id even if the path used a suffix
	in.CreatedAt = existing.CreatedAt
	if err := saveBoard(in); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": "/board/" + in.ID, "id": in.ID})
}

// apiPhoto handles GET /api/photo?q=… returning Openverse references as JSON.
func apiPhoto(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(searchPhotos(q))
}

func apiBoards(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loadBoards())
}

// clientURL derives the base URL for the API from the -addr flag value.
func clientURL(addr string) string {
	host := addr
	if h, ok := strings.CutPrefix(addr, "0.0.0.0"); ok {
		host = h
	}
	if host == "" || strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return "http://" + host
}

// apiCall POSTs/PUTs a JSON payload and decodes {url,id}.
func apiCall(method, url string, payload any) (string, string) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		log.Fatal("bad request: ", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal("server unreachable — is the idea-lab service running?")
	}
	defer resp.Body.Close()
	var out struct{ URL, ID string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.ID == "" {
		log.Fatalf("%s failed (%s)", method, resp.Status)
	}
	return out.URL, out.ID
}

// findBoardID does a best-effort match of query against board IDs.
func findBoardID(query string) string {
	_, body, err := apiGet("http://127.0.0.1:8899", "/api/boards")
	if err != nil || body == nil {
		return ""
	}
	var boards []Board
	json.Unmarshal(body, &boards)
	for _, b := range boards {
		if b.ID == query || strings.HasSuffix(b.ID, query) {
			return b.ID
		}
	}
	return ""
}

// apiGet fetches a path from the local server, returning status + body.
func apiGet(base, path string) (int, []byte, error) {
	resp, err := http.Get(base + path)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// clientCreate / clientEdit / clientList drive the server via its API.
func clientCreate(base, title, sub, bullets, prompt, imageURL string) {
	payload := map[string]any{"title": title, "subtitle": sub, "imagePrompt": prompt, "imageUrl": imageURL}
	if bullets != "" {
		payload["bullets"] = strings.Split(bullets, ";")
	}
	urlStr, id := apiCall(http.MethodPost, base+"/api/board", payload)
	fmt.Printf("board created: %s%s\n", lanHost(), urlStr)
	_ = id
}

func clientEdit(base, idQuery, title, sub, bullets, prompt, imageURL, imgPath string) {
	payload := map[string]any{"title": title, "subtitle": sub, "imagePrompt": prompt, "imageUrl": imageURL, "imagePath": imgPath}
	if bullets != "" {
		payload["bullets"] = strings.Split(bullets, ";")
	}
	id := findBoardID(idQuery)
	if id == "" {
		log.Fatal("no board matches '", idQuery, "' — try ./idea-lab ls")
	}
	urlStr, _ := apiCall(http.MethodPut, base+"/api/board/"+id, payload)
	fmt.Printf("board updated: %s%s\n", lanHost(), urlStr)
}

func clientList(base string) {
	_, raw, done := apiCallList(base + "/api/boards")
	if done != nil {
		log.Fatal("server unreachable — is the idea-lab service running?")
	}
	var boards []Board
	json.Unmarshal(raw, &boards)
	for _, b := range boards {
		img := b.ImagePath
		if img == "" && b.ImageURL != "" {
			img = "remote:" + b.ImageURL
		}
		if img == "" {
			img = "(placeholder)"
		}
		fmt.Printf("%s  %-24s %s\n", b.ID, b.Title, img)
	}
}

func lanHost() string {
	return "http://192.168.1.58:8899"
}

func apiCallList(url string) (int, []byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func main() {
	addr := flag.String("addr", "0.0.0.0:8899", "listen address")
	flag.Parse()
	args := flag.Args()

	usage := func() {
		fmt.Fprintln(os.Stderr, `usage:
  idea-lab [-addr 0.0.0.0:8899]                      # run the server
  idea-lab new "Title" [-sub …] [-bullets "a;b;c"] [-prompt …] [-imgurl …]
  idea-lab edit <id-suffix> [-title …] [-sub …] [-bullets …] [-prompt …] [-imgpath /img/x.png]
  idea-lab ls`)
		os.Exit(2)
	}
	if len(args) == 0 {
		serve(*addr)
		return
	}

	// Re-parse remaining args with the verb-specific flag sets.
	base := clientURL(*addr)
	verb := args[0]
	rest := args[1:]

	switch verb {
	case "ls":
		clientList(base)
	case "new", "edit":
		editMode := verb == "edit"
		flags, positional := parseVerbArgs(rest)
		if editMode && len(positional) == 0 {
			usage()
		}
		// NOTE: hand-rolled parsing on purpose — Go's flag package stops at
		// the first positional arg, which swallowed every flag after the
		// title/id. Do not "simplify" this back to flag.NewFlagSet.
		title := flags["title"]
		if len(positional) > 0 && title == "" && !editMode {
			title = positional[0]
		}
		if title == "" && !editMode {
			usage()
		}
		if editMode {
			idQuery := positional[0]
			cur := fetchBoard(idQuery)
			if cur == nil {
				log.Fatal("no board matches '", idQuery, "' — try ./idea-lab ls")
			}
			subV := firstNonEmpty(flags["sub"], cur.Subtitle)
			bV := firstNonEmpty(flags["bullets"], strings.Join(cur.Bullets, ";"))
			pV := firstNonEmpty(flags["prompt"], cur.ImagePrompt)
			iu := firstNonEmpty(flags["imgurl"], cur.ImageURL)
			// New prompt (and no explicit image override) => regenerate.
			// Unchanged prompt => keep the existing illustration.
			ip := ""
			switch {
			case flags["imgpath"] != "":
				ip = flags["imgpath"]
			case flags["imgurl"] != "":
				ip = ""
			case flags["prompt"] == "" || flags["prompt"] == cur.ImagePrompt:
				ip = cur.ImagePath
			}
			titleV := firstNonEmpty(title, cur.Title)
			clientEdit(base, idQuery, titleV, subV, bV, pV, iu, ip)
			return
		}
		clientCreate(base, title, flags["sub"], flags["bullets"], flags["prompt"], flags["imgurl"])
	default:
		usage()
	}
}

// parseVerbArgs splits raw args into flag values and positionals.
// "-flag value" pairs are consumed in order; a flag without a following
// value gets "".
func parseVerbArgs(args []string) (map[string]string, []string) {
	flags := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[name] = args[i+1]
				i++
			} else {
				flags[name] = ""
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// fetchBoard returns the current board for an id or id-suffix query.
func fetchBoard(query string) *Board {
	_, body, err := apiGet("http://127.0.0.1:8899", "/api/boards")
	if err != nil || body == nil {
		return nil
	}
	var boards []Board
	json.Unmarshal(body, &boards)
	for i := range boards {
		id := boards[i].ID
		if id == query || strings.HasSuffix(id, query) {
			return &boards[i]
		}
	}
	return nil
}

// serve runs the HTTP server.
func serve(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", idxPage)
	mux.HandleFunc("GET /board/", boardPage)
	mux.HandleFunc("POST /api/board", apiBoard)
	mux.HandleFunc("PUT /api/board/{id}", apiBoardEdit)
	mux.HandleFunc("GET /api/boards", apiBoards)
	mux.HandleFunc("GET /api/photo", apiPhoto)
	mux.Handle("GET /img/", http.FileServer(http.Dir(dir)))

	log.Printf("idea-lab serving on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
