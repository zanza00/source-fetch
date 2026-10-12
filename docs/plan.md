# source-fetch — piano di realizzazione

> Derivato da `docs/the_idea.md` (§1–§14). Decisioni prese con l'utente:
> **riscrittura da zero** dallo spec (nessun sorgente `ctx` disponibile),
> **Go** come linguaggio, **Linux + macOS + Windows** come piattaforme target,
> **nessuna retrocompatibilità** (l'utente ha due progetti consumatori e migra
> manualmente alla nuova incarnazione).

## 1. Linguaggio: Go

Il requisito Linux/macOS/Windows esclude bash (assente su Windows senza WSL).
Go offre binario statico singolo, cross-compile `GOOS/GOARCH`, Windows nativo e
zero runtime: rispetta lo spirito di §11 ("drop su qualsiasi macchina").

## 2. Scostamenti rispetto allo spec (Windows + no-backcompat)

- **Parsing TOML**: niente `yq`/fallback `awk` (§4); una libreria TOML nativa
  sostituisce l'intero ramo `US`-separated. Un solo parser.
- **`version_cmd`**: in bash era `eval`; in Go si esegue via shell di piattaforma
  (`sh -c` su unix, `cmd /C` su Windows). Shell-dependent, documentato.
- **Cache id**: niente `cksum` (BSD/GNU, assente su Windows) → SHA-256 troncato.
- **Cache root**: `SOURCE_FETCH_CACHE` → `XDG_CACHE_HOME` → `os.UserCacheDir()`
  (Linux `~/.cache`, macOS `~/Library/Caches`, Windows `%LocalAppData%`).
- **git**: invocato come binario esterno (Git per Windows è comune), per
  preservare esattamente shallow clone `--branch`, fetch di SHA e `rev-parse`.
- **Legacy rimosso**: niente `$CTX_*`, `.ctx-ref`, `[tool.*]`, `tools.toml`,
  migrazione `~/.cache/chezmoi-context`, shim `ctx`.

## 3. Layout

```
source-fetch/
├── go.mod
├── cmd/source-fetch/main.go        # entrypoint sottile → cli.Run
├── internal/
│   ├── cli/                        # list|path|<name>|update|rm|clean|manifest|help
│   ├── manifest/                   # modello entry, load TOML, discovery
│   ├── resolve/                    # installed_version + precedenza ref (§5)
│   ├── fetch/                      # algoritmo fetch + at_ref + marker (§7)
│   ├── cache/                      # cache root, manifest-id, layout, guardie clean (§6)
│   └── git/                        # wrapper subprocess git
├── manifest.example.toml
├── docs/{manifest.md,transports.md}
└── test/{run.sh,fixtures/}
```

## 4. Mapping spec → moduli

| Spec | Modulo | Note |
|---|---|---|
| §3 CLI/stdout-stderr contract | `cli` | `path` stdout = solo il path, progress su stderr |
| §4 manifest + parsing | `manifest` | TOML nativo, solo `[source.*]` |
| §4/§9 discovery | `manifest` | `$SOURCE_FETCH_MANIFEST` → walk-up `.source-fetch.toml` → `~/.config/source-fetch/manifest.toml` |
| §5 precedenza ref | `resolve` | CLI ref → `ref` → version tag → `default_ref` → branch |
| §6 cache layout/guardie | `cache` | id = basename toplevel git, else `path-<hash>` |
| §7 fetch + at_ref + marker | `fetch` | marker `.source-fetch-ref` |
| §12 transport | `fetch` | campo `transport="git"` + interfaccia `Transport` |

## 5. Seam per §12

Campo opzionale `transport = "git"` (default) e nome canonico `url` che sussume
`repo` (accettato come alias). `version_from` per lockfile/npm resta fuori scope
ma non bloccato dal modello.

## 6. Fasi

| Fase | Deliverable | Verifica |
|---|---|---|
| 0 | Decisioni congelate + `go.mod` | build sui 3 OS |
| 1 | `main` + `cli` skeleton | unit test |
| 2 | `manifest`: load TOML + discovery | fixture TOML |
| 3 | `cache` + `git` + `fetch`/`at_ref` | bare repo locali, no rete |
| 4 | `resolve`: versione + precedenza | casi §5 |
| 5 | Comandi completi | §13 end-to-end |
| 6 | Suite test (unit + `test/run.sh`) | 9 criteri §13 |
| 7 | Docs + `manifest.example.toml` | — |
| 8 | CI matrix + release | artefatti |
| 9 | Migrazione dei due consumatori | risolvono con la nuova CLI |

## 7. Decisioni congelate

1. git esterno (non `go-git`).
2. Campo canonico `url` (+`transport="git"`), `repo` accettato come alias.
3. Cache root: `SOURCE_FETCH_CACHE` → `XDG_CACHE_HOME` → `os.UserCacheDir()`.
4. Discovery: `.source-fetch.toml` (canonico) + `~/.config/source-fetch/manifest.toml`.
5. Distribuzione: GitHub Releases via goreleaser + `go install`.
