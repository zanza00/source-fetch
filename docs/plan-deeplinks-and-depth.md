# Piano di implementazione: Deep-link in `peek` (#1) e controllo profondità Git (#5)

> **Nota (2026-10-22):** questo documento è conservato come storico. La versione
> riveduta, con decisioni vincolanti e correzioni verificate sul comportamento
> reale di git, è in [`plan-deeplinks-and-depth-v2.md`](plan-deeplinks-and-depth-v2.md).
> Usare quella per l'implementazione.

Questo documento definisce il piano di implementazione per due evoluzioni di `source-fetch`:

1. **Supporto diretto ai deep-link web in `peek` e `rm`** (GitHub, GitLab, Codeberg/Forgejo/Gitea).
2. **Controllo della profondità del clone (`--deep` / `--depth <N>`)** per abilitare `git log` e `git blame` quando necessario, mantenendo `--depth 1` come default.

---

## Parte 1 — Deep-link web in `peek` (e `rm`)

### 1.1 Obiettivo e comportamento atteso

Utenti e agenti incollano spesso URL copiati dal browser che puntano a un file, una directory, un tag o un commit specifico:

- **GitHub / Gitea**:
  - `https://github.com/owner/repo/blob/<ref>/<subpath>#L42-L50`
  - `https://github.com/owner/repo/tree/<ref>/<subpath>`
  - `https://github.com/owner/repo/commit/<sha>`
- **GitLab** (inclusi gruppi e sottogruppi annidati):
  - `https://gitlab.com/group/sub/repo/-/blob/<ref>/<subpath>#L10`
  - `https://gitlab.com/group/sub/repo/-/tree/<ref>/<subpath>`
  - `https://gitlab.com/group/sub/repo/-/commit/<sha>`
- **Codeberg / Forgejo / Gitea**:
  - `https://codeberg.org/owner/repo/src/branch/<ref>/<subpath>`
  - `https://codeberg.org/owner/repo/src/tag/<ref>/<subpath>`
  - `https://codeberg.org/owner/repo/src/commit/<sha>/<subpath>`

**Contratto CLI:**

- `source-fetch peek <web-url> [cli-ref]`:
  1. Estrae `RepoURL`, `Ref` (se presente nell'URL) e `Subpath` (se presente), scartando query (`?plain=1`) e fragment (`#L42`).
  2. Calcola il nome in cache (`cache.AdhocName`) a partire da `RepoURL` normalizzato, così due deep-link a file diversi della stessa repo condividono lo **stesso** checkout in `<adhoc-root>/<repo>-<hash>`.
  3. Esegue il checkout al ref risolto (`cli-ref` esplicito ha priorità sul `Ref` estratto dall'URL).
  4. Se il deep-link include un `Subpath`, stampa su `stdout` `<dir>/<subpath>` (stesso contratto di `source-fetch path <name> <subpath>`); altrimenti stampa `<dir>`.
- `source-fetch rm <web-url>`:
  - Normalizza `<web-url>` a `RepoURL` prima di calcolare `cache.AdhocName`, permettendo di rimuovere la cache anche passando lo stesso deep-link usato con `peek`.

### 1.2 Design tecnico (`internal/resolve/weburl.go`)

Introdurre nel pacchetto `internal/resolve` una struttura e una funzione pura di parsing:

```go
type ParsedURL struct {
    RepoURL string // URL clonabile del repository
    Ref     string // branch, tag o SHA estratto dall'URL (vuoto se assente)
    Subpath string // percorso interno al repository con '/' (vuoto se assente)
}

func ParseWebURL(rawURL, cliRef string) ParsedURL
```

**Regole di parsing:**

1. **Passthrough per URL non-HTTP(S)**:
   - Path locali, `file://`, `ssh://` e `git@...` vengono restituiti invariati (`RepoURL: rawURL`).
2. **Pulizia preliminare**:
   - Rimozione di `#fragment` (es. `#L42-L50`) e `?query` (es. `?plain=1`).
3. **Riconoscimento dei marker di percorso web**:
   - **GitLab (`/-/`)**:
     - Pattern: `/<namespace...>/<repo>/-/(blob|tree|commit)/<rest>`
     - Tutto ciò che precede `/-/` è `<namespace...>/<repo>`, supportando sottogruppi arbitrari.
   - **Codeberg / Forgejo / Gitea (`/src/`)**:
     - Pattern: `/<owner>/<repo>/src/(branch|tag|commit)/<rest>`
   - **GitHub / GitLab legacy (`/blob/`, `/tree/`, `/commit/`)**:
     - Pattern: `/<owner>/<repo>/(blob|tree|commit)/<rest>`
4. **Estrazione di `Ref` e `Subpath` da `<rest>`**:
   - Per `commit/<sha>[/<subpath>]`: il primo segmento è `Ref` (`<sha>`), il resto eventuale è `Subpath`.
   - Per `blob|tree|branch|tag`:
     - Se `cliRef != ""` ed è prefisso di `<rest>` (`<cliRef>` o `<cliRef>/<subpath>`), usa `cliRef` come `Ref` e la parte rimanente come `Subpath` (gestisce senza ambiguità branch con `/`, es. `release/1.0`).
     - Altrimenti il primo segmento di `<rest>` è `Ref` e i segmenti successivi formano `Subpath`.
5. **Validazione di sicurezza sul `Subpath`**:
   - Verificare `filepath.IsLocal(filepath.FromSlash(Subpath))` per impedire path traversal (`..`) fuori dalla directory di cache.

### 1.3 Modifiche ai file per la Parte 1

| File | Modifica |
|---|---|
| `internal/resolve/weburl.go` | Nuovo file con `ParsedURL` e `ParseWebURL`. |
| `internal/resolve/weburl_test.go` | Test table-driven per GitHub, GitLab (anche sottogruppi), Codeberg, commit URL, `#L42`, `?plain=1`, `cliRef` con `/`, e path traversal guard. |
| `internal/cli/cli.go` | In `cmdPeek`: usare `resolve.ParseWebURL(url, cliRef)` e stampare `filepath.Join(dir, filepath.FromSlash(parsed.Subpath))` quando presente. In `cmdRm`: normalizzare con `ParseWebURL(target, "").RepoURL`. |
| `internal/cli/cli_test.go` | Test end-to-end di `cmdPeek` e `cmdRm` con URL deep-link. |
| `internal/skills/data/read/SKILL.md` | Aggiornare la guida spiegando che `peek` accetta direttamente URL `/blob/...` e `/tree/...` e stampa già il path del file/directory. |
| `skills/source-fetch/SKILL.md` | Allineare la nota sintetica su `peek`. |
| `README.md` | Documentare il supporto ai link web diretti in `peek`. |

---

## Parte 2 — Controllo profondità Git (`--deep` / `--depth <N>`)

### 2.1 Obiettivo e comportamento atteso

Per default `source-fetch` usa `--depth 1` per velocizzare il download e minimizzare lo spazio su disco. Quando però serve ispezionare la storia (`git log -S`, `git log -L`, `git blame`), l'agente o l'utente deve poter richiedere più storia senza dover clonare il repo manualmente altrove.

**Flag supportati** sui comandi che effettuano o aggiornano il checkout (`path`, `<name> [ref]`, `peek`, `update`):

- *(nessun flag)* → default attuale (`--depth 1`).
- `--deep` → storia completa (nessun `--depth` in clone, oppure `git fetch --unshallow` se già presente in cache).
- `--depth <N>` → storia limitata a `<N>` commit (`N >= 1`).

### 2.2 Design tecnico (`internal/fetch`)

1. **Rappresentazione della profondità**:
   - Definire in `internal/fetch`:
     - `DepthDefault = 1` (shallow clone a 1 commit)
     - `DepthFull = -1` (clone completo, attivato da `--deep`)
     - `depth > 1` (shallow clone a `N` commit, attivato da `--depth N`)
2. **Rilevamento stato shallow e Marker**:
   - Aggiungere in `internal/fetch`:
     ```go
     // IsShallow riporta se il checkout in dir è uno shallow repository (.git/shallow esiste).
     func IsShallow(dir string) bool
     ```
   - Estendere `Marker` (`.source-fetch-ref`) con un campo opzionale `Depth int` (`depth=1`, `depth=50`, `depth=full` / `0` per retrocompatibilità coi marker esistenti che vengono trattati come `Depth: 1` se `.git/shallow` esiste).
3. **Interazione con il fast-path (`Ensure`)**:
   - Se `HasGit(dir) && AtRef(dir, ref) && !force`:
     - Se la cache non è shallow (`!IsShallow(dir)`), soddisfa già qualsiasi richiesta (`--deep` o `--depth N`) → **ritorno immediato**.
     - Se la richiesta è `DepthDefault` (`1`) → **ritorno immediato** (comportamento invariato).
     - Se la cache è shallow ma viene richiesto `--deep` (`DepthFull`):
       - Esegue upgrade in-place: `git -C <dir> fetch -q --unshallow` e aggiorna il marker. Se il comando fallisce, cade nel re-clone completo.
     - Se la cache è shallow con profondità inferiore al `--depth N` richiesto:
       - Esegue `git -C <dir> fetch -q --depth <N>` e aggiorna il marker (con fallback al re-clone).
4. **Clone iniziale e `fetchCommit`**:
   - Costruire gli argomenti `--depth` dinamicamente:
     - `depth == DepthFull`: omette `--depth` sia in `git clone` sia in `fetchCommit`.
     - `depth >= 1`: passa `--depth <strconv.Itoa(depth)>`.

### 2.3 Parsing dei flag in `internal/cli`

`internal/cli/cli.go` dispone già di `parseFlags(args, knownFlags)`.
Basta definire un helper condiviso per i comandi di fetch (`cmdFetch`, `cmdPath`, `cmdPeek`, `cmdUpdate`):

```go
var fetchFlags = map[string]bool{
    "deep":  false,
    "depth": true,
}
```

- Se `--deep` e `--depth` vengono passati insieme, restituisce un errore chiaro (`cannot use --deep and --depth together`).
- Valida che `--depth <N>` sia un intero `>= 1`.
- Nota sulla guardia esistente in `dispatch`: se `args[0]` inizia con `-` (e non è `-v`/`--version`/`-h`/`--help`), continua a restituire `unknown option`; i flag `--deep` / `--depth` vengono passati dopo il comando o il nome della sorgente (es. `source-fetch path chezmoi --deep`, `source-fetch peek <url> --deep`, `source-fetch chezmoi --depth 20`).

### 2.4 Modifiche ai file per la Parte 2

| File | Modifica |
|---|---|
| `internal/fetch/fetch.go` | Estendere `Ensure` (o aggiungere `EnsureOpts`) per supportare `depth`, implementare `IsShallow`, upgrade in-place con `--unshallow` / `--depth N`, e aggiornare `Marker`. |
| `internal/fetch/fetch_test.go` | Aggiungere `makeRepoWithHistory(t, nCommits)` e testare: clone default (`depth=1`), upgrade in-place a `--deep` e `--depth N` sul fast-path, e clone diretto `--deep`. |
| `internal/cli/cli.go` | Integrare il parsing di `--deep` e `--depth <N>` in `cmdFetch`, `cmdPath`, `cmdPeek`, `cmdUpdate` e aggiornare `usage()`. |
| `internal/cli/cli_test.go` | Testare le combinazioni di flag CLI e gli errori di validazione (`--depth 0`, `--depth abc`, `--deep` + `--depth`). |
| `internal/skills/data/read/SKILL.md` | Aggiungere nota su `--deep` / `--depth <N>` quando serve usare `git log` o `git blame`. |
| `internal/skills/data/core/SKILL.md` | Aggiornare la tabella comandi e la sezione Checkouts. |
| `README.md` | Aggiornare la sezione Usage. |
| `test/run.sh` | Aggiungere smoke test offline per `--deep` e `peek` con deep-link. |

---

## Ordine di esecuzione consigliato e verifica

1. **Step 1 — Parte 1 (`ParseWebURL` + `peek`/`rm`)**:
   - Implementare `internal/resolve/weburl.go` e `internal/resolve/weburl_test.go`.
   - Collegare `cmdPeek` e `cmdRm` in `internal/cli/cli.go` + test in `internal/cli/cli_test.go`.
   - Verifica: `go test ./internal/resolve/... ./internal/cli/...`
2. **Step 2 — Parte 2 (`depth` in `internal/fetch` + flag CLI)**:
   - Aggiornare `internal/fetch/fetch.go` e `internal/fetch/fetch_test.go`.
   - Esporre `--deep` e `--depth` in `internal/cli/cli.go` e `internal/cli/cli_test.go`.
   - Verifica: `go test ./internal/fetch/... ./internal/cli/...`
3. **Step 3 — Documentazione, Skill embedded e Smoke test**:
   - Aggiornare `README.md`, `skills/source-fetch/SKILL.md`, `internal/skills/data/{read,core}/SKILL.md`.
   - Estendere `test/run.sh`.
   - Verifica finale completa: `make fmt && make vet && make test && make smoke`.
