# Piano di implementazione: `version_bin` / `version_arg` (#2) e `version_file` / `version_match` (#4)

Questo documento definisce il piano di implementazione per risolvere insieme:

- **Punto #2 (Sicurezza ed ergonomia per i binari CLI)**: sostituire il comando shell libero `version_cmd` con la coppia strutturata `version_bin` + `version_arg` (con default `"--version"`), eseguita direttamente con `exec.CommandContext` senza passare per alcuna shell (`sh -c` / `cmd /C`).
- **Punto #4 (Version-matching per librerie e lockfile di progetto)**: introdurre la coppia speculare `version_file` + `version_match` per leggere passivamente la versione da `go.mod`, `package.json`, `Cargo.toml`, `Cargo.lock`, `pyproject.toml`, `uv.lock`, `mise.toml`, `.nvmrc`, ecc., senza eseguire alcun processo.

---

## 1. Problema e simmetria del nuovo design

Oggi `source-fetch` risolve dinamicamente la versione solo eseguendo `version_cmd` tramite `sh -c` (o `cmd /C` su Windows) in `internal/resolve/resolve.go`.
Questo crea due problemi speculari quando `.source-fetch.toml` è committato in un repository di progetto:

1. **Superficie di attacco (Punto #2)**: la discovery automatica risale da `$PWD`. Un `.source-fetch.toml` malevolo in un repo clonato da terzi può concatenare comandi arbitrari in `version_cmd` (es. `chezmoi --version; ...`) o invocare uno script locale (`./scripts/evil.sh`).
2. **Inadeguatezza per le librerie (Punto #4)**: una libreria non installa un binario in `$PATH`. La sua versione è già dichiarata in un file del progetto (`go.mod`, `package.json`, `Cargo.lock`, `uv.lock`, ecc.).

Separando in modo simmetrico le due modalità si ottiene un modello dichiarativo, uniforme e privo di parsing shell:

| Modalità | Campo principale (target) | Campo secondario (opzionale, con default) |
|---|---|---|
| **Binario CLI (#2)** | `version_bin = "chezmoi"` | `version_arg = "--version"` *(default: `"--version"` se omesso)* |
| **File / Lockfile (#4)** | `version_file = "go.mod"` | `version_match = "github.com/BurntSushi/toml"` *(default: primo `X.Y.Z` nel file)* |

Vantaggi immediati di `version_bin` + `version_arg`:
- **Zero parser di virgolette o metacaratteri shell**: `exec.CommandContext(ctx, bin, arg)` riceve direttamente il nome del binario e il singolo argomento.
- **Meno boilerplate nel 95% dei casi**: poiché quasi tutti i tool CLI usano `--version`, basta scrivere `version_bin = "chezmoi"` senza nemmeno specificare `version_arg`.
- **Retrocompatibilità morbida**: esattamente come `repo` è accettato come alias legacy di `url` in `internal/manifest/manifest.go`, un eventuale `version_cmd = "chezmoi --version"` nei manifest esistenti può essere convertito automaticamente in `version_bin` + `version_arg` durante `manifest.Load` (e riscritto nella forma canonica da `Save()`).

---

## 2. Parte A — `version_bin` + `version_arg` (#2)

### 2.1 Regole di sicurezza ed esecuzione

In `internal/resolve/resolve.go` sostituire `runShell` con l'esecuzione diretta del binario:

1. **Nessuna shell (`sh -c` / `cmd /C` rimossi)**:
   - Se `e.VersionBin == ""` ritorna `""`.
   - Se `e.VersionArg == ""`, usa il default `"--version"`.
   - Invoca `exec.CommandContext(ctx, e.VersionBin, e.VersionArg)` direttamente.
2. **Solo binari semplici da `$PATH` (no script del repo)**:
   - `version_bin` deve essere un singolo nome di eseguibile: non deve contenere `/`, `\`, né spazi o caratteri di controllo, non deve iniziare con `-` e non può essere `.` o `..`.
   - In questo modo `exec.LookPath(e.VersionBin)` cerca esclusivamente nelle directory del `$PATH` dell'utente e non può mai eseguire `./evil.sh` dentro la directory di lavoro.
3. **Timeout di esecuzione**:
   - Eseguire il comando con `context.WithTimeout(context.Background(), 2*time.Second)` e `cmd.Stdin = nil` per evitare blocchi se un binario attende input da terminale.

### 2.2 Validazione e alias legacy nel Manifest

In `internal/manifest/manifest.go`:

- **Validazione in `Entry.Validate()`**:
  - Se `VersionBin != ""`: rifiutare se contiene `/`, `\`, whitespace o inizia con `-`.
  - Se `VersionArg != ""` e `VersionBin == ""`: restituire errore (`version_arg requires version_bin`).
- **Compatibilità in lettura per `version_cmd`**:
  - In `manifest.Load`, se `version_bin` è vuoto e il TOML contiene il campo legacy `version_cmd` (con 1 o 2 parole semplici senza `/` o metacaratteri shell, es. `"chezmoi --version"`), popola `VersionBin = fields[0]` e `VersionArg = fields[1]`.
  - In `manifest.Save()`, `version_cmd` non viene più scritto (proprio come `repo`), migrando il file a `version_bin` / `version_arg` (e omettendo `version_arg` quando è uguale al default `"--version"` o vuoto).

---

## 3. Parte B — Risoluzione dichiarativa da file (`version_file` + `version_match`) (#4)

### 3.1 Nuovi campi in `.source-fetch.toml`

Estendere `[source.<name>]` con due campi opzionali:

| Campo | Obbligatorio | Significato |
|---|---|---|
| `version_file` | no | Percorso relativo alla directory del manifest da cui leggere la versione (es. `"go.mod"`, `"package.json"`, `"Cargo.lock"`, `".nvmrc"`). |
| `version_match` | no | Stringa o regex per individuare la riga/sezione del pacchetto dentro `version_file` (richiede `version_file`). |

**Esempi pratici multi-ecosistema:**

```toml
# Go (go.mod)
[source.toml]
url           = "https://github.com/BurntSushi/toml.git"
version_file  = "go.mod"
version_match = "github.com/BurntSushi/toml"
tag_prefix    = "v"

# Node / TypeScript (package.json)
[source.zod]
url           = "https://github.com/colinhacks/zod.git"
version_file  = "package.json"
version_match = '"zod"'
tag_prefix    = "v"

# Rust (Cargo.lock — blocco multi-riga name + version)
[source.serde]
url           = "https://github.com/serde-rs/serde.git"
version_file  = "Cargo.lock"
version_match = 'name = "serde"'
tag_prefix    = "v"

# File a versione singola (.nvmrc, .node-version, VERSION)
[source.node]
url          = "https://github.com/nodejs/node.git"
version_file = ".nvmrc"
tag_prefix   = "v"
```

### 3.2 Algoritmo di estrazione (`VersionFromFile`)

1. **Risoluzione sicura del path**:
   - `version_file` è validato in `Entry.Validate()` con `filepath.IsLocal(filepath.FromSlash(e.VersionFile))` (impedisce path assoluti e traversal `../`).
   - Il path effettivo è `filepath.Join(e.ManifestDir, filepath.FromSlash(e.VersionFile))` (con fallback a `.` se `ManifestDir` è vuoto nei test unitari).
   - Lettura limitata (es. `io.LimitReader` a 4 MB) per evitare allocazioni eccessive su file errati; se il file non esiste o non è leggibile, ritorna `""` (stesso fallback morbido di `version_cmd`).
2. **Modalità 1 — Senza `version_match` (`version_match == ""`)**:
   - Restituisce `versionRe.FindString(content)` (primo `X.Y.Z` nel file).
3. **Modalità 2 — Con `version_match` contenente un gruppo di cattura regex `(...)`**:
   - Se `version_match` contiene `(` e `)` e compila come regex con `re.NumSubexp() >= 1`:
     - Esegue `re.FindStringSubmatch(content)`.
     - Se trova match, estrae `versionRe.FindString(submatch[1])` (o `submatch[1]` se già valido).
4. **Modalità 3 — Con `version_match` testuale (caso predefinito per `go.mod`, `package.json`, `Cargo.toml`, `Cargo.lock`, `uv.lock`, `mise.toml`)**:
   - Scansiona il file riga per riga cercando la prima riga che contiene `version_match` (`strings.Contains(line, e.VersionMatch)`):
     - Cerca `versionRe.FindString(line)` sulla riga stessa (copre `go.mod`, `package.json`, `Cargo.toml`, `mise.toml`, `.tool-versions`).
     - Se sulla riga stessa non c'è `X.Y.Z`, ispeziona fino alle **3 righe successive** fermandosi se incontra una riga vuota o un nuovo header `[` (copre i blocchi `[[package]]` di `Cargo.lock` e `uv.lock` dove `name = "..."` è seguito da `version = "X.Y.Z"`).

### 3.3 Precedenza di risoluzione dei Ref

La catena in `resolve.Ref(e, cliRef)` diventa:

1. **CLI `ref`** (argomento esplicito)
2. **Manifest `ref`** (pin statico)
3. **Version tag** (`tag_prefix` + versione da **`version_file`** (+ `version_match`), oppure da **`version_bin`** (+ `version_arg`) se `version_file` non è configurato/non produce risultato)
4. **Manifest `default_ref`**
5. **Default branch** (`""`)

### 3.4 Integrazione CLI (`manifest add` e `manifest set`)

- `source-fetch manifest add <name> <url>`:
  - Aggiungere i flag `--version-bin <bin>`, `--version-arg <arg>`, `--version-file <path>` e `--version-match <pattern>` (mantenendo `--version-cmd` come alias deprecato o sostituendolo).
- `source-fetch manifest set <name> <key>=<value>...`:
  - Accettare le chiavi `version_bin`, `version_arg`, `version_file` e `version_match` (valore vuoto azzera il campo, come per gli altri campi).

---

## 4. File da modificare

| File | Modifica |
|---|---|
| `internal/manifest/manifest.go` | Sostituire/affiancare `VersionCmd` con `VersionBin`, `VersionArg`, `VersionFile`, `VersionMatch` e `ManifestDir` in `Entry`; validare `filepath.IsLocal` per `version_file`, che `version_match` richieda `version_file`, che `version_arg` richieda `version_bin`, e che `version_bin` sia un nome singolo senza `/` o whitespace; gestire l'alias legacy `version_cmd` in `Load` e scrivere solo i campi canonici in `Save()`. |
| `internal/manifest/manifest_test.go` | Testare caricamento, migrazione dell'alias `version_cmd`, salvataggio round-trip e rifiuto di `version_file = "../secret"`, `version_match` o `version_arg` orfani, e `version_bin = "./evil.sh"`. |
| `internal/resolve/resolve.go` | Sostituire `runShell` con `VersionFromBin` (`exec.CommandContext(ctx, bin, arg)` con default `"--version"` e timeout 2s); implementare `VersionFromFile`; aggiornare `InstalledVersion` per usare `version_file` → `version_bin`. |
| `internal/resolve/resolve_test.go` | Test table-driven per `VersionFromFile` (`go.mod`, `package.json`, `Cargo.lock`, `.nvmrc`, regex con capture group) e per `VersionFromBin` (funziona anche su Windows senza `sh`). |
| `internal/cli/cli.go` | Aggiungere `--version-bin`, `--version-arg`, `--version-file` e `--version-match` a `cmdManifestAdd`, le corrispondenti chiavi a `setEntryField`, e aggiornare `usage()`. |
| `internal/cli/cli_test.go` | Testare `manifest add` / `manifest set` con `version_bin`/`version_arg` e `version_file`/`version_match`. |
| `docs/manifest.md` | Documentare le due coppie simmetriche (`version_bin` + `version_arg` e `version_file` + `version_match`). |
| `internal/skills/data/manifest/SKILL.md` | Aggiornare la guida agente con le due coppie di campi ed esempi per CLI, `go.mod`, `package.json`, `Cargo.lock` e `pyproject.toml`/`uv.lock`. |
| `README.md` & `manifest.example.toml` | Aggiornare gli esempi con `version_bin` e `version_file`. |
| `test/run.sh` | Aggiornare lo smoke test per verificare sia `version_bin` sia `version_file`. |

---

## 5. Ordine di esecuzione e verifica

1. **Step 1 — Modello e validazione Manifest (`internal/manifest`)**:
   - Aggiungere `VersionBin`, `VersionArg`, `VersionFile`, `VersionMatch`, `ManifestDir`, l'alias legacy `version_cmd` in lettura e le regole in `Entry.Validate()`.
   - Verifica: `go test ./internal/manifest/...`
2. **Step 2 — `VersionFromBin` e `VersionFromFile` (`internal/resolve`)**:
   - Implementare `VersionFromBin` (con default `version_arg = "--version"` e timeout 2s) e `VersionFromFile`.
   - Verifica: `go test ./internal/resolve/...`
3. **Step 3 — CLI (`manifest add` / `manifest set`)**:
   - Esporre flag e chiavi in `internal/cli/cli.go`.
   - Verifica: `go test ./internal/cli/...`
4. **Step 4 — Documentazione, Skill embedded e Smoke test**:
   - Aggiornare `docs/manifest.md`, `internal/skills/data/manifest/SKILL.md`, `README.md`, `manifest.example.toml` e `test/run.sh`.
   - Verifica finale: `make fmt && make vet && make test && make smoke`.
