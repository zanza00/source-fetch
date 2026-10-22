# Piano B rivisto — deep-link web e profondità Git

Data: 2026-10-22
Stato: implementato; correzioni della review applicate.
Sostituisce: `docs/plan-deeplinks-and-depth.md` (conservato come storico).
Scope: solo Plan B. `docs/plan-version-resolution-and-safety.md` (Plan A,
`version_*`) resta fuori scope.

---

## 0. Esito dell'indagine

Il piano originale era implementabile, ma non era ancora "usabile" per due
motivi: alcune affermazioni non corrispondono al comportamento reale di git, e
alcune decisioni erano lasciate aperte. Le correzioni principali sono:

1. **La chiave della cache ad-hoc non è canonica.** `cache.AdhocName` hasha la
   stringa originale. `peek .../r.git` e `rm .../r/blob/main/x` calcolano nomi
   diversi. Va introdotta una canonicalizzazione usata da entrambi.
2. **La precedenza di `cliRef` nel piano è invertita.** Il ref esplicito deve
   vincere sempre; il prefix matching serve solo a togliere un ref con `/`
   già noto dal subpath.
3. **`ParseWebURL` non ha un contratto d'errore.** Deve fallire su URL web non
   supportati, path traversal e URL malformati, invece di clonare una pagina
   web.
4. **GitLab legacy non è parsabile in modo affidabile** con il pattern
   proposto. Si supporta solo la forma moderna `/-/`; la forma legacy produce
   un errore esplicito.
5. **`git fetch --depth N` non è innocuo.** Su un checkout già shallow a
   profondità maggiore, **riduce** la storia (verificato: da 2 a 1 commit).
   Serve una regola di no-op basata sullo stato reale, non solo sul marker.
6. **I test con path locali non testano lo shallow clone.** `git clone --depth`
   è ignorato per i path locali; i test devono usare `file://`.
7. **Il deep-link è testabile offline** senza server HTTP, usando
   `url.<base>.insteadOf` in un `GIT_CONFIG_GLOBAL` temporaneo.

---

## 1. Decisioni vincolanti

| # | Decisione | Motivazione |
|---|---|---|
| D1 | `cache.AdhocName` canonicalizza tramite `cache.AdhocKey`. `cmdRm` risolve prima il manifest, poi l'adhoc (URL, path locali, scp-like); un nome relativo nudo resta un nome di manifest, a meno che non esista come directory | `peek` e `rm` devono trovare la stessa entry, ma un nome di manifest non deve essere ombreggiato da un path locale |
| D2 | HTTP(S), SSH (`ssh://`) e scp-like (`git@host:path`) restano chiavi distinte | Hanno semantica del path diversa (scp è relativo alla home, `ssh://` è assoluto): unificarli rischierebbe di riusare la cache sbagliata |
| D3 | `ParseWebURL` ritorna `(ParsedURL, error)` | Un URL web non riconosciuto non deve diventare un clone silenzioso |
| D4 | `ref := cmp.Or(cliRef, parsed.Ref)`; il prefix matching di `cliRef` serve solo a separare ref con `/` dal subpath | Un ref esplicito deve sempre vincere |
| D5 | Subpath: `path.Clean` + `filepath.IsLocal` + verifica esistenza + `EvalSymlinks` dentro il checkout | Evita path traversal e symlink che escono dalla cache |
| D6 | GitLab legacy (`/group/repo/blob/...`) non supportato: errore esplicito | Il pattern non è distinguibile da un repo con sottogruppi; meglio fallire che clonare la pagina sbagliata |
| D7 | Il marker `depth=` è informativo; lo stato reale (`--is-shallow-repository` e `rev-list --count HEAD`) è autoritativo | Il marker può essere stale se l'utente usa git manualmente |
| D8 | Upgrade in-place; se fallisce, il checkout esistente resta intatto | Non distruggere una cache funzionante per un errore di rete |
| D9 | `update --deep` / `update --depth N` = re-clone forzato alla profondità richiesta | Mantiene la semantica attuale di `update` (force) |
| D10 | Test depth con URL `file://`; test deep-link con `insteadOf` + `GIT_CONFIG_GLOBAL` temporaneo | Offline, deterministico, niente rete |
| D11 | Le vecchie cache ad-hoc non vengono migrate automaticamente | La cache è disposable; migrare richiederebbe una scansione con git. Documentare la possibile orphan |
| D12 | I flag vanno dopo comando/nome; `source-fetch --deep <name>` è supportato instradando al comando default | Piccola comodità, senza cambiare la dispatch degli altri comandi |
| D13 | Exit code invariato: 1 per ogni errore | Non introdurre un cambio globale di contratto in questa PR |

---

## 2. Parte B1 — deep-link web in `peek` e `rm`

### 2.1 Forme supportate

**Supportate (con ref e subpath opzionali):**

| Host | Forme |
|---|---|
| GitHub | `/{o}/{r}`, `/{o}/{r}/(blob\|raw\|blame)/{ref}/{path}`, `/{o}/{r}/tree/{ref}[/{path}]`, `/{o}/{r}/commit/{sha}[/{path}]`, `/{o}/{r}/releases/tag/{tag}` |
| GitLab | `/{ns...}/{r}/-/blob/{ref}/{path}`, `/-/tree/{ref}[/{path}]`, `/-/raw/{ref}/{path}`, `/-/blame/{ref}/{path}`, `/-/commit/{sha}[/{path}]`, `/-/tags/{tag}`, `/-/releases/{tag}` |
| Codeberg / Forgejo / Gitea | `/{o}/{r}/src/(branch\|tag)/{ref}[/{path}]`, `/{o}/{r}/src/commit/{sha}[/{path}]`, `/{o}/{r}/commit/{sha}[/{path}]`, `/{o}/{r}/(raw\|blame)/(branch\|tag\|commit)/{ref}/{path}`, `/{o}/{r}/releases/tag/{tag}` |
| Host generico | `/-/...` (GitLab self-hosted), `/(raw\|blame)/(branch\|tag\|commit)/...` e `/src/(branch\|tag\|commit)/...` (Gitea self-hosted; il selettore Gitea vince quando il ref si chiama `branch`, `tag` o `commit`), `/(blob\|tree\|commit\|raw\|blame)/...` dopo `/{owner}/{repo}` (GHE/GitHub-like senza base path) |
| Non web | path locali, `file://`, `ssh://`, `git@...`: passthrough |

**Non supportate (errore esplicito o passthrough documentato):**

- GitHub issues/pulls/actions/search/tags e simili: errore.
- GitLab: `/-/` è riservato. Solo `blob`, `tree`, `commit`, `raw`, `blame`,
  `tags` e `releases` sono supportati; qualunque altra pagina `/-/` (issues,
  commits, compare, branches, pipelines) e le pagine root-level vengono
  rifiutate con un errore esplicito. La forma legacy senza `/-/` è rifiutata su
  `gitlab.com`; su host generico può essere letta come link GitHub-like (se la
  forma lo consente) o come repo/path e fallire al clone. Non è una garanzia.
- Bitbucket: solo URL repository `/{workspace}/{repo}`; `src`, `raw`, commits,
  pull request e wiki sono rifiutati. Azure DevOps: solo URL repository
  `.../_git/{repo}` (forma legacy `{org}.visualstudio.com` inclusa); i deep link
  con `?path=` sono rifiutati. `raw.githubusercontent.com` è rifiutato: è un CDN
  di file, non un host di repository.
- GHE con base path: con un web marker l'URL viene rifiutato (`unsupported URL
  form`); senza marker viene letto come path di repository e il clone può
  fallire. Non è un deep link supportato.
- Host generico con repository a un solo segmento: l'URL repository
  (`/{repo}`) è supportato, ma i deep link (`/{repo}/blob/...`,
  `/{repo}/src/...`, `/{repo}/-/tree/...`) sono ambigui con un repository a due
  segmenti e vengono rifiutati. Usare l'URL del repository.
- Ref con `/`: nelle pagine file/tree senza `cliRef` si usa il primo segmento
  come ref e il resto come subpath; per disambiguare: `peek <url> release/1.0`.
  Nelle pagine tag/release tutto ciò che segue il marker è il tag, quindi
  `releases/tag/release/1.0` e `/-/tags/release/1.0` funzionano.
- Ref non validi (spazi, `..`, caratteri di controllo, `-` iniziale, `:`):
  `git.ValidateRef` li rifiuta prima del fetch; non c'è fallback al branch
  default.

### 2.2 API e algoritmo

```go
// Package resolve
type ParsedURL struct {
    RepoURL string // URL clonabile, senza query/fragment
    Ref     string // branch/tag/SHA estratto dall'URL ("" se assente)
    Subpath string // path interno con '/', già pulito ("" se assente)
}

// ParseWebURL non fa rete. Un input non HTTP(S) viene restituito in RepoURL
// senza modifiche. Un URL HTTP(S) riconosciuto come pagina web non supportata
// restituisce un errore.
func ParseWebURL(rawURL, cliRef string) (ParsedURL, error)
```

Passi:

1. `raw := strings.TrimSpace(rawURL)`; vuoto → errore.
2. Se non inizia per `http://`/`https://` (case-insensitive) → `{RepoURL: raw}`, `nil`.
3. `url.Parse`; errore → `invalid URL %q: %w`.
4. Schema/host normalizzati; host vuoto → errore.
5. Spezzare `u.Path` in segmenti senza `path.Clean` globale: togliere gli `/`
   iniziali/finali, scartare i segmenti vuoti e `.`, ma **conservare** `..`.
   I segmenti del repo (prima del marker) non possono contenere `..`: errore.
   Questo evita che `path.Clean` cancelli il marker o trasformi un traversal
   in un repo diverso.
6. Riconoscere lo stile **in base all'host** (non cercando i marker in ordine
   globale: un path GitHub può contenere una directory chiamata `-`).

   - `github.com`:
     - `len == 2` → repo semplice;
     - `segments[2]` in `blob|tree|commit|raw|blame|releases` → GitHub-like;
     - altrimenti errore (issues, pulls, search, tags…).
   - `codeberg.org` / `gitea.com`:
     - `len == 2` → repo semplice;
     - `segments[2]` in `src|raw|blame|commit|releases` → Gitea;
     - altrimenti errore. Sono accettati sia `/src/commit/{sha}` sia
       `/commit/{sha}`.
   - `gitlab.com`:
     - `/-/` è riservato: la prima occorrenza di `-` (a qualunque indice,
       anche 0) seguita da un segmento diverso da `-` apre una pagina; repo =
       `segments[:i]`;
     - se il kind non è tra `blob|tree|commit|raw|blame|tags|releases`, errore
       esplicito (copre issues, commits, compare, branches, pipelines e le
       pagine root-level);
     - se i segmenti di repo sono meno di 2, errore `missing repository path`;
     - senza marker, un marker legacy a indice ≥ 2 seguito da almeno un
       segmento produce un errore esplicito;
     - altrimenti repo = tutti i segmenti (sottogruppi).
   - `raw.githubusercontent.com`: errore (è un CDN di file, non un host di
     repository).
   - `bitbucket.org`: solo `len == 2` (URL repository); ogni path più profondo
     è un errore, perché Bitbucket serve source/raw/commits/PR/wiki sotto lo
     stesso prefisso.
   - Azure DevOps (`dev.azure.com`, `*.visualstudio.com`): richiede un URL con
     `/_git/{repo}`; un `?path=` nella query è un errore, perché la query viene
     sempre scartata e verrebbe stampata la root invece del file.
   - host sconosciuto:
     - se la forma è `/{repo}/{marker}/...` (un repository a un solo segmento
       seguito da un web marker) → errore, perché è indistinguibile da un
       repository a due segmenti. Eccezione: un marker GitLab `/-/` dopo almeno
       due segmenti di repo è inequivocabile e viene lasciato al parser GitLab;
     - provare i candidati in quest'ordine, accettando il primo valido:
       1. Gitea raw/blame con selettore esplicito
          (`segments[2]` in `raw|blame` e `segments[3]` in
          `branch|tag|commit`): il selettore vince anche su un ref GitHub
          Enterprise chiamato letteralmente `branch`, `tag` o `commit`;
       2. GitHub-like (`segments[2]` marker e forma dopo il marker valida);
       3. Gitea (`src|raw|blame|commit|releases` con selettore valido);
       4. GitLab `/-/` (kind supportato, almeno un segmento di repo);
     - se nessun candidato è valido ma un segmento a indice ≥ 2 è un web
       marker noto
       (`blob|tree|commit|commits|compare|src|raw|blame|issues|pulls|actions|search|settings|merge_requests|releases|tags`),
       errore `unsupported URL form` (copre GHE con base path e Bitbucket);
     - altrimenti repo = tutti i segmenti.

   Esempi di ambiguità risolti dal "primo candidato valido":
   - `github.com/o/r/blob/main/dir/-/file`: host noto → GitHub, il `/-/` nel
     path non conta;
   - `host/o/r/blob/main/dir/-/file`: candidato GitHub valido → GitHub;
   - `host/group/sub/repo/-/tree/main`: candidato GitLab valido → GitLab;
   - `host/group/blob/-/tree/main`: candidato GitHub non valido (kind `-`),
     candidato GitLab valido → GitLab;
   - `host/o/blob/main`: repository a un solo segmento con deep link ambiguo →
     errore;
   - `host/o/r/raw/branch/main/file`: selettore Gitea raw/branch → Gitea.

   Nota: per un host sconosciuto che è in realtà GitLab, un URL legacy senza
   `/-/` può essere interpretato come repo/path. È un limite documentato; non
   tentare euristiche basate sui nomi dei segmenti.
7. Estrarre `(urlRef, subpath)`:
   - `commit/<sha>[/<path>]` (GitHub/GitLab/Gitea), `src/commit/<sha>[/<path>]`
     e `/(raw|blame)/commit/<sha>[/<path>]` (Gitea): ref = `<sha>`,
     subpath = resto;
   - `blob|tree|raw|blame/<refAndPath>` (GitHub/GitLab): `splitRefAndPath`;
   - `src/(branch|tag)/<refAndPath>` e
     `(raw|blame)/(branch|tag)/<refAndPath>` (Gitea): `splitRefAndPath`;
   - `releases/tag/<tag>` o `/-/tags/<tag>` o `/-/releases/<tag>`: ref = tag,
     subpath vuoto. Tutto ciò che segue il marker è il tag, quindi i tag con
     `/` sono supportati;
   - marker senza ref (es. `blob` con resto vuoto): errore.
8. Validare il ref con `git.ValidateRef`: un ref non valido è un errore e non
   deve mai diventare un fallback al branch default. La validazione copre
   refspec injection (`:`), opzioni (`-` iniziale), revision expression (`..`,
   `@{`), caratteri di controllo, spazi e path invalidi.
9. `splitRefAndPath(parts, cliRef)`:

```go
if cliRef != "" && hasPathPrefix(parts, strings.Split(cliRef, "/")) {
    return cliRef, join(parts[len(strings.Split(cliRef, "/")):])
}
if len(parts) == 0 { return "", "" }
return parts[0], join(parts[1:])
```

Il chiamante fa poi `ref := cmp.Or(cliRef, parsed.Ref)`. Attenzione: per gli
URL `commit/<sha>` il ref resta lo SHA e il subpath è ciò che segue lo SHA;
`cliRef` può sovrascrivere il ref, ma non deve consumare lo SHA come subpath.

10. Pulizia subpath:

```go
sub = path.Clean(strings.Join(subParts, "/"))
if sub == "." { sub = "" }
if sub != "" {
    if strings.HasPrefix(sub, "../") || sub == ".." || strings.HasPrefix(sub, "/") ||
       strings.ContainsRune(sub, 0) ||
       !filepath.IsLocal(filepath.FromSlash(sub)) {
        return ParsedURL{}, fmt.Errorf("path %q escapes the repository", sub)
    }
}
```

11. `RepoURL`: ricostruire con schema/host normalizzati e path del repo, senza
    query/fragment; mantenere userinfo e porta (servono al clone). Per host
    noti il repo ha almeno 2 segmenti; per host sconosciuti è ammesso anche un
    solo segmento, ma i deep link a un solo segmento sono rifiutati (vedi
    sopra).

Errori consigliati (minuscoli, senza punteggiatura finale):

```text
invalid URL "…": …
unsupported GitHub URL "…": only repository, blob, tree, commit, raw, blame and releases/tag links are supported
unsupported GitLab URL "…": only repository, blob, tree, commit, raw, blame, tags and releases links are supported
unsupported GitLab URL "…": legacy /blob/ links are not supported; use the /-/blob/ form
unsupported URL form "…": base paths and this host are not supported; use the repository URL
unsupported URL form "…": single-segment repository deep links are not supported; use the repository URL
unsupported Bitbucket URL "…": only repository links (/{workspace}/{repo}) are supported; …
unsupported Azure DevOps URL "…": deep links with ?path= are not supported; use the repository URL
invalid ref "…" in URL "…": …
URL "…" has no ref after /blob/
path "../../etc/passwd" escapes the repository
```

### 2.3 Canonicalizzazione della cache ad-hoc

In `internal/cache`:

```go
// AdhocKey restituisce l'identità stabile di un URL one-off. Non è
// necessariamente un URL clonabile.
func AdhocKey(raw string) string

// AdhocName usa AdhocKey per l'hash e per il basename.
func AdhocName(raw string) string
```

Implementazione: riconoscere la forma scp-like (`user@host:path`) **prima** di
`url.Parse`, perché Go rifiuta `git@host:path` con
`first path segment in URL cannot contain colon`. Per il resto usare
`net/url`; per gli URL HTTP(S) ricostruire la chiave da schema, host, porta e
path, ignorando userinfo/query/fragment. Se `url.Parse` fallisce, usare una
pulizia testuale di fallback (trim di query/fragment e `/` finale) e hashare
quella: `AdhocName` non può restituire un errore, quindi non deve panicare né
perdere l'entry.

Regole di `AdhocKey`:

- HTTP(S): schema e host lowercase; porta default rimossa (`:80` per http,
  `:443` per https); userinfo esclusa dalla chiave; query/fragment rimossi;
  path con `path.Clean`; `/` finale rimosso; case del path preservato.
  Il suffisso `.git` (case-insensitive) viene rimosso **solo per host noti**
  (`github.com`, `gitlab.com`, `codeberg.org`, `gitea.com`), dove è
  convenzionalmente opzionale. Su host sconosciuti resta nella chiave: meglio
  una cache duplicata che un falso positivo tra due endpoint diversi.
- `ssh://`: come sopra, porta default 22, username incluso nella chiave;
  `.git` rimosso. Il path resta assoluto.
- scp-like `user@host:path`: forma distinta, con prefisso `scp://` nella chiave
  per non confonderla con `ssh://`; path `path.Clean` senza forzare lo `/`
  iniziale, `.git` rimosso. `git@github.com:o/r.git` e
  `ssh://git@github.com/o/r` restano quindi chiavi diverse (documentato).
- `file://`: path con `path.Clean`; **non** rimuovere `.git` (una directory
  locale può chiamarsi davvero `x.git`).
- path locale: `filepath.Clean`; nessuna rimozione di `.git`.
- Altri schemi: schema/host lowercase, path pulito; nessuna pretesa di
  equivalenza con HTTP/SSH.

Conseguenze:

```text
https://github.com/o/r.git
https://github.com/o/r/
HTTPS://GitHub.COM:443/o/r?x=1#L2
→ stessa AdhocKey / AdhocName

https://github.com/o/r
git@github.com:o/r
ssh://git@github.com/o/r
→ tre chiavi diverse (documentato)

git@github.com:o/r.git
git@github.com:o/r/
→ stessa chiave scp

ssh://git@github.com:22/o/r.git
ssh://git@github.com/o/r
→ stessa chiave ssh
```

`AdhocName` mantiene il formato `<basename>-<8 hex>`; il basename viene dalla
chiave canonica, non dall'URL originale. Per il solo nome visualizzato, un
suffisso `.git` finale viene rimosso anche quando la chiave lo conserva: così
`file:///tmp/bare.git` continua a produrre `bare-<hash>`, mentre l'hash resta
distinto da `file:///tmp/bare`.

### 2.4 Modifiche CLI

`cmdPeek`:

```go
pos, flags, err := parseFlags(args, fetchFlags)
// 0 pos → list; >2 pos → errore
depth, err := parseDepthFlags(flags)
cliRef := ""
if len(pos) > 1 { cliRef = pos[1] }
parsed, err := resolve.ParseWebURL(pos[0], cliRef)
ref := cmp.Or(cliRef, parsed.Ref)
e := &manifest.Entry{Name: cache.AdhocName(parsed.RepoURL), URL: parsed.RepoURL, Transport: "git"}
dir, err := fetch.Ensure(root, e, ref, fetch.EnsureOpts{Depth: depth, Progress: progress})
if parsed.Subpath != "" {
    dir, err = subpathPath(dir, parsed.Subpath)
}
fmt.Println(dir)
```

`cmdRm` risolve prima il manifest e solo dopo l'adhoc:

```go
// 1. Un nome di manifest vince, così `rm <name>` resta il comando di progetto.
m, manifestErr := loadManifest()
if manifestErr == nil {
    if e, ok := m.Lookup(target); ok {
        root, err := cache.Root(m.Path)
        return removeCached(root, e.Name, target)
    }
}

// 2. Nessuna entry: solo i target ad-hoc passano al fallback. Un nome nudo
//    sconosciuto mantiene la diagnosi del manifest (errore chiaro, niente
//    rimozione per errore di battitura).
if !isAdhocTarget(target) {
    if manifestErr != nil { return manifestErr }
    return fmt.Errorf("unknown source %q", target)
}

root, err := cache.AdhocRoot()
parsed, err := resolve.ParseWebURL(target, "")
return removeCached(root, cache.AdhocName(parsed.RepoURL), target)
```

`isAdhocTarget` accetta ciò che `peek` può aver creato:

- qualunque stringa con `://` (HTTP, SSH, `file://`);
- path Windows (`C:`, `C:/x`, `C:\x`);
- URL scp-like (`cache.IsSCPLike`: `[user@]host:path`);
- path assoluti, `./`, `~/`, o contenenti `/` o `\`;
- un path che esiste già su disco.

Un nome relativo nudo (`repo`) è quindi un nome di manifest, a meno che non
esista davvero come directory: in caso di collisione con una entry usare
`./repo` per indirizzare il path locale. `peek` e `rm` devono usare lo stesso
spelling perché `AdhocKey` canonicalizza, ma non inventa un path diverso.

`subpathPath` (condiviso da `cmdPeek` e `cmdPath`):

1. `sub = filepath.Clean(filepath.FromSlash(sub))`; se `sub == "."` → `dir`.
2. Ridifendere `filepath.IsLocal(sub)`; se falso → errore traversal.
3. `p := filepath.Join(dir, sub)`.
4. `os.Stat(p)`: se non esiste → `path %q not found in checkout %s`.
5. `realDir := filepath.EvalSymlinks(dir)`, `realP := filepath.EvalSymlinks(p)`.
6. `filepath.Rel(realDir, realP)`: se `..` o prefisso `../` → errore
   `path %q resolves outside the checkout`.
7. Stampare il path lessicale `p` (stabile), non quello risolto.

Nota: `filepath.IsLocal` da solo è lessicale; il controllo con `EvalSymlinks`
è quello che copre i symlink.

### 2.5 File toccati in B1

| File | Modifica |
|---|---|
| `internal/resolve/weburl.go` | Nuovo: `ParsedURL`, `ParseWebURL`, helper di split/validazione |
| `internal/resolve/weburl_test.go` | Tabella host/forme/errori; passthrough; traversal; `cliRef` con `/` |
| `internal/cache/cache.go` | `AdhocKey`, `AdhocName` canonico, `IsSCPLike` |
| `internal/cache/cache_test.go` | Tabella equivalenze/differenze; stabilità; `IsSCPLike` |
| `internal/git/ref.go` | Nuovo: `ValidateRef` (rifiuta opzioni, refspec, revision expression, path/ref invalidi) |
| `internal/git/git_test.go` | Tabella `ValidateRef` |
| `internal/cli/cli.go` | `cmdPeek`, `cmdRm`, `isAdhocTarget`, `subpathPath`, uso in `cmdPath` |
| `internal/cli/cli_test.go` | Deep-link offline con `insteadOf`; rm canonico, path locali e scp-like; errori; single-segment |
| `internal/skills/data/read/SKILL.md` | Incollare direttamente il link; `peek` stampa già il path |
| `skills/source-fetch/SKILL.md` | Nota sintetica su deep-link |
| `README.md` | Sezione deep-link e tabella forme supportate |

---

## 3. Parte B2 — profondità del clone

### 3.1 Modello

```go
const (
    DepthDefault = 1  // comportamento attuale
    DepthFull    = -1 // --deep
)

type EnsureOpts struct {
    Force    bool
    Depth    int // 0 = DepthDefault; DepthFull o >= 1
    Progress Progress
}

func Ensure(root string, e *manifest.Entry, ref string, opts EnsureOpts) (string, error)
```

`Ensure` normalizza `Depth == 0` in `DepthDefault`; valori `< DepthFull` sono
un errore. Questo rende sicuro lo zero value.

### 3.2 Marker

Estendere `.source-fetch-ref`:

```text
ref=v1.2.3
commit=…
fetched=…
depth=1|N|full
```

- `depth=full` ↔ `DepthFull`.
- Marker vecchio senza `depth=` → `Marker.Depth == 0` (sconosciuto).
- `ReadMarker` ignora valori non validi.
- `WriteMarker(dir, ref, depth)` scrive `full` se `!IsShallow(dir)`, altrimenti
  la profondità richiesta (o `DepthDefault` se 0).
- La profondità registrata è mostrata nella colonna `DEPTH` di `list` e `peek`:
  `full`, il numero di commit, oppure `-` per un marker vecchio o invalido.
- **La soddisfazione della profondità non usa il marker**: usa lo stato reale.
  Il marker serve a `list`/debug e alla retrocompatibilità.

### 3.3 Stato e no-op

```go
func IsShallow(dir string) bool {
    out, err := git.Run(dir, "rev-parse", "--is-shallow-repository")
    if err == nil { return out == "true" }
    _, err = os.Stat(filepath.Join(dir, ".git", "shallow"))
    return err == nil
}

func depthSatisfied(dir string, want int) bool {
    if want == DepthFull { return !IsShallow(dir) }
    if want <= 1 { return true }
    if !IsShallow(dir) { return true }
    n, err := commitCount(dir) // git rev-list --count HEAD
    return err == nil && n >= want
}
```

Regole (per lo stesso ref; il fast path richiede `AtRef(dir, ref)`):

- richiesta `DepthDefault` → sempre soddisfatta;
- cache completa → soddisfa qualsiasi richiesta, anche `--depth N` (non si
  riduce mai la storia);
- cache shallow con `N` commit disponibili ≥ richiesti → no-op;
- cache shallow con meno commit → upgrade;
- se il ref richiesto cambia, `AtRef` è falso: il checkout viene ri-clonato
  alla profondità richiesta, sostituendo la cache precedente.

### 3.4 Fast path e upgrade

```go
if !force && HasGit(dir) && AtRef(dir, ref) {
    if depthSatisfied(dir, depth) { return dir, nil }
    if err := deepen(dir, depth, progress); err != nil {
        return "", fmt.Errorf("cannot deepen cached checkout %s: %w", dir, err)
    }
    if err := WriteMarker(dir, ref, depth); err != nil { return "", err }
    return dir, nil
}
```

`deepen` non rimuove mai `dir`:

```text
--deep  → git -C <dir> fetch -q --unshallow origin <target>
--depth N → git -C <dir> fetch -q --depth N origin <target>
```

Il `target` si deriva sempre dal checkout corrente, **non** da `ref`:

```go
func deepenTarget(dir string) string {
    if branch, err := git.Run(dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && branch != "" {
        return branch
    }
    if sha, err := git.HeadCommit(dir); err == nil {
        return sha
    }
    return "HEAD"
}
```

Motivo: `AtRef(dir, "")` restituisce sempre true, quindi la cache può essere
detached su un tag o SHA anche quando il ref risolto è vuoto. Fare
`fetch origin HEAD` approfondirebbe il branch default remoto, non il commit
checked out. Con un branch attaccato si usa il nome del branch; con HEAD
detached si usa lo SHA corrente. Questo copre anche i casi in cui il marker è
stale rispetto a HEAD: si approfondisce sempre la storia che l'utente sta per
leggere. Il refspec esplicito è necessario per tag/SHA. `--unshallow` e
`--depth N` sono verificati con `depthSatisfied` dopo il fetch; se il risultato
non basta, errore.

Rischio SHA: con HEAD detached su uno SHA, `git fetch origin <sha>` dipende
dalla policy del server (`uploadpack.allowReachableSHA1InWant` /
`uploadpack.allowAnySHA1InWant`). Se il server rifiuta lo SHA, l'upgrade
fallisce ma il checkout esistente resta intatto.

**Mai chiamare `--depth N` quando la cache è già a profondità ≥ N**: git
ridurrebbe la storia (verificato). Il no-op va fatto prima.

Se l'upgrade fallisce: si restituisce errore e si lascia il checkout esistente.
Niente fallback distruttivo. Per ripartire: `update <name>` (re-clone forzato).
Nota per i test: `deepen` usa il remote `origin` già configurato nel checkout,
non `e.URL`; per simulare un errore bisogna rendere irraggiungibile `origin`
(rename/delete del bare o `git remote set-url`), non cambiare solo `e.URL`.

### 3.5 Clone e fetch iniziale

```go
func depthArgs(depth int) []string {
    if depth == DepthFull { return nil }
    return []string{"--depth", strconv.Itoa(depth)}
}
```

- Clone senza ref: `git clone -q [--depth N] -- <url> <dir>`.
- Clone con ref: `git clone -q [--depth N] --branch <ref> -- <url> <dir>`.
- `fetchCommit` (SHA/non-branch): `git fetch -q [--depth N] origin <ref>`,
  poi `checkout --detach FETCH_HEAD`.
- Fallback ref sconosciuto: clone del default branch con la stessa profondità.

`--deep` omette `--depth`; `--depth 1` è identico al default.

### 3.6 Flag CLI

```go
var fetchFlags = map[string]bool{
    "deep":  false,
    "depth": true,
}
```

Helper condiviso:

```go
func parseDepthFlags(flags map[string]string) (int, error)
```

- `--deep` e `--depth` insieme → errore
  `cannot use --deep and --depth together`;
- `--depth` deve essere un intero ≥ 1;
- `--deep=false` è accettato e significa default;
- valori non `true`/`false` per `--deep` → errore.

Comandi:

| Comando | Posizionali | Depth |
|---|---|---|
| `path <name> [subpath]` | nome, subpath | sì |
| `<name> [ref]` | nome, ref | sì |
| `peek <url> [ref]` | URL, ref | sì |
| `update <name>` | nome | sì (force) |

`peek` senza URL continua a listare la cache ad-hoc; se però sono presenti
`--deep`/`--depth`, restituire `missing URL` invece di ignorare i flag.

`dispatch`: se `args[0]` è `--deep` o `--depth...`, instradare a `cmdFetch(args)`
invece di restituire `unknown option`; gli altri flag iniziali restano errori.
Così `source-fetch --deep chezmoi` funziona, mentre `source-fetch --nope`
continua a fallire.

### 3.7 Semantica di `update`

- `update <name>` → re-clone forzato shallow (comportamento attuale).
- `update <name> --depth N` → re-clone forzato a N commit.
- `update <name> --deep` → re-clone forzato completo.

Non è un upgrade in-place: `update` significa "rifai da zero". L'upgrade
in-place è solo il fast path di `path`/`peek`/`<name>`.

### 3.8 File toccati in B2

| File | Modifica |
|---|---|
| `internal/fetch/fetch.go` | `DepthDefault/DepthFull`, `EnsureOpts`, `IsShallow`, `depthSatisfied`, `deepen`, marker depth |
| `internal/fetch/fetch_test.go` | Storia, tag, SHA, no-op, upgrade, failure preservation, marker |
| `internal/git/ref.go` | `ValidateRef`, usato da `Ensure`, `WriteMarker` e dal parser web |
| `internal/git/git_test.go` | Tabella `ValidateRef` (ref validi e invalidi) |
| `internal/cli/cli.go` | `fetchFlags`, `parseDepthFlags`, wiring, `usage`, dispatch |
| `internal/cli/cli_test.go` | Validazione flag, `--deep`/`--depth` end-to-end offline, colonna `DEPTH` |
| `internal/skills/data/read/SKILL.md` | Quando usare `--deep`/`--depth` per `git log`/`git blame` |
| `internal/skills/data/core/SKILL.md` | Tabella comandi, sezione Checkouts, marker depth |
| `README.md` | Usage e sezione storia |
| `test/run.sh` | Smoke `--deep`, `--depth`, deep-link con `insteadOf` |

---

## 4. Test matrix

### B1 — parser e cache

| Caso | Atteso |
|---|---|
| GitHub blob con `#L42` e `?plain=1` | repo, ref, subpath puliti |
| GitHub tree, commit, raw, blame, releases/tag | ref/subpath corretti |
| GitHub repo URL con `.git`, `/` finale | repo, ref/subpath vuoti |
| GitLab sottogruppi `/-/blob`, `/-/tree`, `/-/commit`, `/-/tags`, `/-/releases` | corretti |
| GitLab pagine `/-/` non source (issues, commits, compare, branches, pipelines) e root-level | errore esplicito |
| GitLab legacy `/group/repo/blob/main/x` | errore esplicito |
| GitLab con marker senza repo (`/-/blob`, `group/-/blob`) | errore `missing repository path` |
| Codeberg `src/branch`, `src/tag`, `src/commit` | corretti |
| Codeberg `commit/{sha}[/{path}]` | corretti |
| Codeberg `raw/branch`, `blame/branch`, `releases/tag` | corretti |
| Tag con `/` in `releases/tag/release/1.0`, `/-/tags/release/1.0`, `/-/releases/release/1.0` | ref `release/1.0`, subpath vuoto |
| Host generico con `/-/`, `/src/`, `/blob/` | corretti |
| Host generico `raw/branch`, `blame/branch`, `raw/commit` | Gitea (selettore esplicito vince) |
| GitHub con directory `-` nel path del file | resta GitHub (non GitLab) |
| Host generico `group/sub/repo/-/tree/main` | GitLab |
| Host generico `group/blob/-/tree/main` | GitLab (candidato GitHub non valido) |
| GitLab con repo chiamato `-`: `group/sub/-/-/blob/main/foo` | repo `group/sub/-`, ref `main` |
| Host generico a un solo segmento: `/repo/blob`, `/repo/src`, `/repo/-/tree`, `/o/blob/main` | errore single-segment |
| Host generico con base path (GHE) | errore `unsupported URL form` |
| Bitbucket `/src`, `/raw`, `/commits`; Azure `?path=` | errore esplicito |
| `raw.githubusercontent.com` | errore (CDN di file) |
| Ref non validi: `..`, `%2e%2e`, NUL, newline, spazio, `:`, `@{}`, `-` iniziale | errore `invalid ref`, nessun fallback |
| `cliRef` non-prefisso (`main/foo.go` + `v2`) | ref `v2`, subpath `foo.go` |
| `cliRef` con `/` (`release/1.0/foo.go` + `release/1.0`) | ref `release/1.0`, subpath `foo.go` |
| `..`, `%2e%2e`, path assoluto | errore traversal |
| URL malformato / web non supportato | errore |
| path locale, `file://`, `ssh://`, `git@` | passthrough |
| `AdhocName` su host noto con `.git`, `/`, query, fragment, host case, porta default | stessa chiave |
| `AdhocName` su host sconosciuto con/senza `.git` | chiavi diverse (scelta conservativa) |
| `AdhocName` su URL malformato | fallback stabile, nome non vuoto |
| HTTP vs SSH | chiavi diverse |
| scp vs `ssh://` | chiavi diverse (semantica del path diversa) |
| path con case diverso / porta non default | chiavi diverse |

### B1 — CLI

| Caso | Atteso |
|---|---|
| `peek https://github.com/o/r/blob/main/file.txt` con `insteadOf` | stampa `<dir>/file.txt`, file esiste |
| seconda `peek` dello stesso deep-link | stessa dir, fast path |
| `peek https://github.com/o/r.git` poi `rm https://github.com/o/r/tree/main` | rimuove la stessa cache |
| `peek <path>` / `peek ./<rel>` poi `rm` con lo stesso spelling | rimuove la stessa cache |
| `peek file:///…` | `rm` con lo stesso `file://` rimuove la stessa cache |
| `peek git@host:o/r` | `rm` con lo stesso URL scp-like rimuove la stessa cache |
| `rm <name>` con una entry di manifest | rimuove la cache di progetto, non l'adhoc |
| `rm <bare-name>` senza entry e senza path esistente | errore `unknown source` / manifest, nessuna rimozione |
| `peek https://github.com/o/r/blob/main/nope` | errore "not found", niente path fantasma |
| symlink dentro il repo verso l'esterno | errore "resolves outside the checkout" |
| URL issues/pulls, single-segment, Bitbucket/Azure deep link | errore, nessun clone |
| `cmdPath <name> <subpath>` inesistente | errore (coerenza con peek) |

### B2 — fetch

Fixture: repo con 6+ commit, branch `main`, tag su un commit intermedio, SHA
del tip. URL sempre `file://` per rispettare `--depth`.

| Caso | Atteso |
|---|---|
| default | shallow, 1 commit, marker `depth=1` |
| `--depth 3` iniziale | shallow, 3 commit |
| `--deep` iniziale | completo, tutti i commit |
| upgrade 1 → 3 | stessa dir, 3 commit, HEAD invariato |
| upgrade 3 → 2 | no-op, resta a 3 (non ridurre) |
| upgrade 1 → full | completo, tutti i commit |
| full → `--depth 1` | no-op, resta completo |
| tag a depth 1 → `--depth 3` | 3 commit dell'antenato del tag |
| SHA a depth 1 → `--depth 3` | 3 commit dell'antenato dello SHA |
| tag a depth 1 → `--deep` | storia completa dell'antenato del tag |
| SHA a depth 1 → `--deep` | storia completa dell'antenato dello SHA (se il server lo consente) |
| cambio ref (tag/branch diverso) | ri-clone alla profondità richiesta, non upgrade in-place |
| ref vuoto su checkout detached a tag → `--depth 3` | usa lo SHA del tag, 3 commit (non il branch default) |
| ref vuoto su checkout detached a SHA → `--depth 3` | usa lo SHA corrente, 3 commit |
| ref vuoto su branch attaccato → `--depth 3` | usa il branch corrente, 3 commit |
| upgrade con remote irraggiungibile (rename/delete del bare o `remote set-url` verso un path inesistente) | errore, checkout esistente intatto |
| marker vecchio senza depth | letto come sconosciuto, nessun panic |
| marker `depth=full`, `depth=N`, `depth=bad` | parsing corretto/ignorato |

### B2 — CLI

| Caso | Atteso |
|---|---|
| `--depth 0`, `--depth abc`, `--depth` senza valore | errore chiaro |
| `--deep --depth 2` | errore |
| `--deep=false` | default |
| `path tool --deep`, `tool --depth 2`, `peek <url> --depth 2`, `update tool --deep` | depth applicata |
| `source-fetch --deep tool` | funziona (dispatch default) |
| `source-fetch --nope` | ancora `unknown option` |

### Offline deep-link fixture

Niente server HTTP. In `internal/cli/cli_test.go` e in `test/run.sh`:

```sh
cat >"$tmp/gitconfig" <<EOF
[url "file://$bare"]
    insteadOf = https://github.com/acme/tool
EOF
export GIT_CONFIG_GLOBAL="$tmp/gitconfig"
export GIT_CONFIG_NOSYSTEM=1
```

Poi:

```sh
source-fetch peek https://github.com/acme/tool/blob/main/file.txt
source-fetch rm https://github.com/acme/tool/tree/main
```

`git clone` riscrive l'URL in `file://`, quindi `--depth` funziona davvero.
Creare il fixture **prima** di esportare `GIT_CONFIG_GLOBAL`.

Per i test Go usare un helper che costruisca un URL `file://` portabile:

```go
p := filepath.ToSlash(path)
if !strings.HasPrefix(p, "/") { p = "/" + p } // Windows: C:/… → /C:/…
u := url.URL{Scheme: "file", Path: p}
```

Se il test con `insteadOf` risultasse instabile su Windows, skipparlo lì e
mantenere la copertura parser/unit; lo smoke è già skippato su Windows.

---

## 5. Documentazione

### README

- Aggiunti "Deep link" alle feature.
- Usage:
  ```sh
  source-fetch peek <url|web-link> [ref] [--deep | --depth N]
  source-fetch path chezmoi --deep
  source-fetch chezmoi --depth 20
  source-fetch update chezmoi --deep
  ```
- Sezione "Deep link e storia": tabella forme supportate, esempi, limiti
  (GitLab `/-/` riservato e pagine non source, Bitbucket/Azure repository-only,
  `raw.githubusercontent.com`, GHE con base path, host generico a un solo
  segmento, ref con `/`, ref non validi).
- `rm`: prima il manifest, poi l'adhoc (URL, deep link, path locali, scp-like);
  `./repo` in caso di collisione con un nome di manifest.
- Cache: nota che la chiave ad-hoc è canonica e che HTTP/SSH restano distinte;
  le vecchie cache ad-hoc possono restare orfane.

### Skill embedded

- `internal/skills/data/read/SKILL.md`: il link viene incollato direttamente;
  `peek` stampa già il path del file/directory. `--deep`/`--depth N` servono
  per `git log -S`, `git log -L`, `git blame`. Aggiornata la nota finale: la
  chiave ad-hoc è canonica per `/` finale, query/fragment, host case, porta
  default e `.git` sugli host noti; HTTP, SSH e scp restano distinti. Per i repo
  locali `peek` e `rm` accettano path e `file://`; usare lo stesso spelling.
  `rm` prova prima il manifest, quindi `./repo` se il nome collide con una entry.
- `internal/skills/data/core/SKILL.md`: tabella comandi con i flag; sezione
  Checkouts con default shallow, upgrade same-ref, `update --deep`; marker con
  depth e colonna `DEPTH`; nota su canonicalizzazione e `rm`.
- `skills/source-fetch/SKILL.md`: aggiornato il bullet su `peek`.

### Usage in `cli.go`

Aggiunti i flag alle righe di `path`, `<name>`, `peek`, `update`, più una
breve sezione:

```text
depth:
  default is a shallow clone (1 commit); --deep fetches full history,
  --depth N fetches the last N commits (for git log / git blame).
```

---

## 6. Implementazione

L'implementazione è atterrata come **due commit feature nello stesso PR**,
nell'ordine B1 → B2, seguiti da commit di review che hanno corretto i casi
limite trovati durante la revisione:

1. B1 — `feat(peek): accept web deep links and canonicalize ad-hoc cache keys`:
   parser web, `AdhocKey`/`AdhocName`, `cmdPeek`/`cmdRm`, `subpathPath`, docs
   e skill.
2. B2 — `feat(fetch): add --deep and --depth history controls`:
   `EnsureOpts`, marker depth, flag CLI, colonna `DEPTH`, docs e skill.
3. Commit di review — validazione dei ref, pagine GitLab/Gitea/Codeberg,
   `rm` asimmetrico, marker depth, host non supportati, test e documentazione.

Non due PR separate: entrambe le parti toccano `cmdPeek`, `cmdPath`,
`cmdFetch`, `usage` e le stesse skill, quindi la sequenza B1 → B2 nello stesso
PR evita conflitti e mantiene ogni commit verde.

---

## 7. Criteri di accettazione

### B1

- Un deep-link GitHub/GitLab/Codeberg/Gitea (e le forme generiche supportate)
  a file, directory, commit o tag produce il path locale corretto, senza
  passare il ref a mano quando è nell'URL.
- `peek <repo>.git` e `rm <deep-link>` sulla stessa repo rimuovono la stessa
  cache; `rm` risolve prima il manifest, poi l'adhoc, e accetta URL, deep link,
  path locali e scp-like.
- I web URL riconosciuti come non supportati falliscono con un errore chiaro
  prima del clone. Unico passthrough residuo: GHE con base path e senza web
  marker, che viene letto come path di repository e fallisce al clone.
- Ref non validi falliscono prima del fetch, senza fallback al branch default.
- Subpath inesistenti o che escono dal checkout falliscono.
- Nessuna rete nei test; nessun accesso alla cache utente reale.

### B2

- Default invariato: shallow a 1 commit.
- `--deep` porta a storia completa, anche su tag/SHA quando il server consente
  di fetchare quello SHA.
- Per lo stesso ref, `--depth N` porta ad almeno N commit e non riduce mai una
  cache più profonda. Se il ref cambia, il checkout viene ri-clonato alla
  profondità richiesta.
- Un upgrade fallito non distrugge il checkout esistente.
- `update --deep` re-clona completo; `update` resta shallow; `update` è sempre
  un re-clone forzato.
- La colonna `DEPTH` di `list`/`peek` mostra `full`, `N` o `-` (marker vecchio
  o invalido).
- Marker vecchi restano leggibili.
- Test offline su Linux/macOS/Windows; smoke POSIX su Linux/macOS.

---

## 8. Rischi e note

- **Compatibilità cache ad-hoc**: dopo la canonicalizzazione, le vecchie
  entry possono non essere più raggiungibili con lo stesso URL. Sono
  disposable; `peek` le elenca ancora. Documentare nel rilascio.
- **Ref con `/`**: nelle pagine file/tree senza `cliRef` l'URL è ambiguo.
  Documentare il workaround (`peek <url> release/1.0`); le pagine tag/release
  non sono ambigue perché tutto il resto è il tag.
- **Shallow e merge**: `rev-list --count HEAD` può contare più commit della
  profondità lineare; è comunque una misura di storia disponibile, adatta al
  no-op "almeno N commit".
- **`git fetch --depth` riduce la storia**: la regola di no-op è obbligatoria,
  non un'ottimizzazione. Vale per lo stesso ref: cambiare ref ri-clona.
- **`update` e clone iniziale distruttivi**: restano force/re-clone come prima;
  solo l'upgrade in-place del fast path preserva la cache esistente. Un
  clone-to-temp + rename è un miglioramento separato, non richiesto da B2.
- **Ref validation implementata**: `internal/git/ref.go` (`ValidateRef`) copre
  opzioni, refspec injection (`:`), revision expression (`..`, `@{`), path
  invalidi, caratteri di controllo e spazi. `fetchCommit` resta comunque
  soggetto alla policy del server per il fetch di uno SHA
  (`uploadpack.allowReachableSHA1InWant` / `allowAnySHA1InWant`): se il server
  rifiuta lo SHA, l'operazione fallisce e nessun ref invalido viene usato come
  fallback.
- **GHE con base path**: con un web marker l'URL è rifiutato; senza marker è
  letto come path di repository e fallisce al clone. Non è un deep link
  supportato.
- **Host generico a un solo segmento**: l'URL repository è supportato, i deep
  link no; l'ambiguità con un repository a due segmenti non è risolvibile senza
  rete.
- **Windows**: symlink e `insteadOf` possono richiedere skip mirati; lo smoke
  è già escluso su Windows.
- **Nessuna modifica a Plan A**: non toccare `version_*`, `version_bin`,
  `version_file` o i test relativi.

---

## 9. Verifica finale

```sh
make fmt
go build ./...
go vet ./...
go test ./...
make smoke
```

In più, per B1/B2:

```sh
go test ./internal/resolve/... ./internal/cache/... ./internal/cli/...
go test ./internal/fetch/... ./internal/cli/...
```

Tutti i test devono restare offline e usare `SOURCE_FETCH_CACHE` con una
directory temporanea.

---

## 10. Review amendments

Correzioni applicate rispetto alla prima stesura del piano:

- **GitLab**: `/-/` è riservato a qualunque indice; solo `blob`, `tree`,
  `commit`, `raw`, `blame`, `tags` e `releases` sono supportati. Pagine
  non-source e root-level sono rifiutate con un errore esplicito; la forma
  legacy resta un errore dedicato.
- **Codeberg/Gitea**: accettati sia `/src/commit/{sha}` sia `/commit/{sha}`;
  `/releases/tag` e i tag GitLab con `/` uniscono il resto come tag.
- **Host generico**: il selettore Gitea `raw|blame/(branch|tag|commit)` vince
  sulla forma GitHub-like quando il ref si chiama `branch`, `tag` o `commit`.
- **Ref**: `git.ValidateRef` valida il ref prima di clone/fetch/marker; un ref
  invalido non fa fallback al branch default.
- **`rm`**: risolve prima il manifest, poi l'adhoc; supporta URL, deep link,
  path locali e scp-like (`cache.IsSCPLike`). Un nome relativo nudo resta un
  nome di manifest; `./repo` risolve la collisione.
- **Limiti host**: `raw.githubusercontent.com` rifiutato; Bitbucket e Azure
  solo URL repository (Azure senza `?path=`); deep link generici a un solo
  segmento rifiutati; GHE con base path rifiutato con marker e passthrough che
  fallisce al clone senza marker.
- **Storia**: il "non ridurre mai" vale per lo stesso ref; cambiare ref
  ri-clona alla profondità richiesta. `deepen` usa il target del checkout
  corrente (branch, SHA o HEAD), con il rischio policy-server per gli SHA.
- **Marker/UI**: `depth=` è mostrato nella colonna `DEPTH` di `list` e `peek`
  come `full`, `N` o `-`.
- **Test**: aggiunte le matrici per pagine GitLab, Gitea raw/blame, Codeberg
  commit, tag con `/`, ref invalidi, single-segment, host non supportati,
  `rm` locale/scp e `--deep` su tag/SHA.
- **Consegna**: due commit feature nello stesso PR (B1 poi B2) più i commit di
  review, non due PR separate.
