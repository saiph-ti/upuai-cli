# Padrões do CLI (Go)

## Arquitetura

```
cli/
├── main.go                    # Entry point — chama cmd.Execute()
├── Makefile                   # Build, install, test, lint, fmt
├── cmd/                       # Comandos Cobra
│   ├── root.go                # Root, flags globais, helpers (requireAuth, requireProject, requireServiceConfig, resolveServiceContext, resolveEnvironmentID, getEnvironment, getProjectID)
│   ├── login.go               # OAuth GitHub + Email OTP
│   ├── logout.go              # Limpa credentials
│   ├── whoami.go              # Mostra usuário, org, projeto
│   ├── init.go                # Cria projeto (detecta framework)
│   ├── link.go                # Linka diretório a projeto existente
│   ├── unlink.go              # Deslinka diretório do projeto
│   ├── list.go                # Lista todos os projetos (alias: ls)
│   ├── open.go                # Abre projeto no browser
│   ├── delete.go              # Deleta projeto linkado
│   ├── deploy.go              # Deploy + watch mode (alias: up)
│   ├── redeploy.go            # Redeploy do último deployment
│   ├── rollback.go            # Rollback de deployment
│   ├── promote.go             # Promove entre ambientes
│   ├── down.go                # Remove último deployment (para serviço)
│   ├── status.go              # Status do projeto e serviços
│   ├── add.go                 # Adiciona serviço ao projeto (wizard interativo)
│   ├── restart.go             # Reinicia serviço linkado
│   ├── logs.go                # Visualiza logs do serviço (flags: -n/--lines)
│   ├── scale.go               # Escala réplicas do serviço
│   ├── run.go                 # Executa comando com env vars injetadas (`-s` opcional, `--` opcional; parse manual via DisableFlagParsing)
│   ├── shell.go               # Subshell interativo com env vars do service (paridade `railway shell`)
│   ├── db.go                  # `db connect` (psql interativo) / `db backup` (pg_dump) / `db restore` (pg_restore) — usa endpoint público
│   ├── environment.go         # Gerencia ambientes (alias: env) — subcommands: list, switch, new, delete
│   ├── variables.go           # Gerencia env vars (aliases: vars, variable) — subcommands: list, set, delete; flag `-s/--service` em todos
│   ├── domain.go              # Gerencia domínios custom (alias: domains) — subcommands: list, add, delete
│   ├── completion.go          # Gera scripts de autocompletion (bash|zsh|fish|powershell)
│   ├── upgrade.go             # Atualiza CLI para última versão
│   └── version.go             # Versão, commit, build date
├── internal/
│   ├── api/
│   │   ├── client.go          # HTTP client (doRequest, Get, Post, Put, Delete, GetRaw, token refresh automático, User-Agent)
│   │   ├── auth.go            # Endpoints de autenticação (login, OAuth, email token, /me)
│   │   ├── projects.go        # ListProjects, GetProject, CreateProject, DeleteProject, GetProjectStatus
│   │   ├── deployments.go     # Deploy, ListDeployments, GetDeployment, Rollback, Redeploy, RemoveDeployment
│   │   ├── environments.go    # ListEnvironments, CreateEnvironment, DeleteEnvironment
│   │   ├── services.go        # ListServices, CreateService
│   │   ├── instances.go       # GetLogs, RestartInstance, ScaleInstance
│   │   ├── variables.go       # ListVariables, SetVariables, DeleteVariable
│   │   ├── domains.go         # ListDomains, AddDomain, DeleteDomain
│   │   └── errors.go          # APIError
│   ├── auth/
│   │   ├── oauth.go           # Fluxo OAuth (servidor local, callback, CSRF state)
│   │   └── token.go           # Decode JWT, verificação de expiração
│   ├── config/
│   │   ├── global.go          # Config global (~/.upuai/config.json), Viper, env vars (UPUAI_)
│   │   ├── credentials.go     # CredentialStore (~/.upuai/credentials.json), UPUAI_TOKEN
│   │   └── project.go         # ProjectConfig (.upuai/config.json), auto-gitignore
│   ├── detect/
│   │   ├── frameworks.go      # Lista de 11 frameworks suportados
│   │   └── detector.go        # DetectFramework, ListDetectedFrameworks
│   ├── ui/
│   │   ├── colors.go          # Paleta (Carmesim, Petroleo, VerdeMusgo, etc)
│   │   ├── styles.go          # Estilos lipgloss (Bold, Dim, Success, Error, etc)
│   │   ├── output.go          # PrintJSON, PrintKeyValue, PrintSuccess/Error/Warning/Info
│   │   ├── prompt.go          # Confirm, SelectOne, InputText, PrintBanner (usa charmbracelet/huh)
│   │   ├── spinner.go         # RunWithSpinner (usa Bubble Tea)
│   │   └── table.go           # Table com headers, rows, auto-width
│   └── watcher/
│       └── watcher.go         # File watcher com fsnotify, debounce, ignore patterns
└── pkg/
    └── version/
        └── version.go         # Version, Commit, BuildDate (injetados via ldflags)
```

## Categorias de Comandos

| Categoria | Comandos |
|-----------|----------|
| **Auth** | `login`, `logout`, `whoami` |
| **Projeto** | `init`, `link`, `unlink`, `list` (ls), `open`, `delete`, `status` |
| **Deploy** | `deploy` (up), `redeploy`, `rollback`, `promote`, `down` |
| **Serviço** | `add`, `restart`, `logs`, `scale`, `run`, `shell` |
| **Database** | `db connect` (psql), `db backup` (pg_dump), `db restore` (pg_restore) |
| **Ambiente** | `environment` (env) → `list`, `switch`, `new`, `delete` |
| **Configuração** | `variables` (vars/variable) → `list`, `set`, `delete` · `domain` (domains) → `list`, `add`, `delete` |
| **Utilitário** | `version`, `completion`, `upgrade` |

## Adicionando Novo Comando

1. Crie `cmd/<command>.go`
2. Siga o template:

```go
package cmd

import (
    "fmt"
    "github.com/upuai-cloud/cli/internal/api"
    "github.com/upuai-cloud/cli/internal/ui"
    "github.com/spf13/cobra"
)

var myCmd = &cobra.Command{
    Use:   "my-command",
    Short: "Descrição curta",
    Long:  `Descrição longa com detalhes de uso.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        // 1. Verificar auth se necessário
        if err := requireAuth(); err != nil {
            return err
        }
        // 2. Verificar projeto se necessário
        projectID, err := requireProject()
        if err != nil {
            return err
        }
        // 3. Chamar API com spinner
        client := api.NewClient()
        var result *api.SomeType
        err = ui.RunWithSpinner("Loading...", func() error {
            var apiErr error
            result, apiErr = client.SomeMethod(projectID)
            return apiErr
        })
        if err != nil {
            return fmt.Errorf("action failed: %w", err)
        }
        // 4. Output formatado
        format := getOutputFormat()
        if format == ui.FormatJSON {
            ui.PrintJSON(result)
            return nil
        }
        ui.PrintKeyValue("Key", result.Value)
        return nil
    },
}

func init() {
    myCmd.Flags().StringVar(&myFlag, "flag-name", "default", "Description")
    rootCmd.AddCommand(myCmd)
}
```

**Padrão obrigatório**:
- Use `RunE` (não `Run`) para retornar erros
- Registre o comando no `init()` com `rootCmd.AddCommand()`
- Use `requireAuth()` e `requireProject()` do `root.go`
- Use `requireServiceConfig()` para comandos que operam em um serviço específico (logs, restart, scale, variables, domain, run)
- Use `getOutputFormat()` e `getEnvironment()` do `root.go`

## Padrão de Subcomandos

Para comandos com subcomandos (como `variables`, `domain`, `environment`):

```go
package cmd

import "github.com/spf13/cobra"

// Comando pai — sem RunE próprio
var parentCmd = &cobra.Command{
    Use:     "parent",
    Aliases: []string{"p"},
    Short:   "Manage resources",
}

// Subcomando list
var parentListCmd = &cobra.Command{
    Use:   "list",
    Short: "List resources",
    RunE: func(cmd *cobra.Command, args []string) error {
        // implementação...
        return nil
    },
}

// Subcomando add
var parentAddCmd = &cobra.Command{
    Use:   "add <name>",
    Short: "Add a resource",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        // implementação...
        return nil
    },
}

func init() {
    parentCmd.AddCommand(parentListCmd)
    parentCmd.AddCommand(parentAddCmd)
    rootCmd.AddCommand(parentCmd)
}
```

**Convenção**: subcomandos são registrados no comando pai via `parentCmd.AddCommand()`, e o pai é registrado no root.

## Helpers do root.go

| Helper | Retorno | Descrição |
|--------|---------|-----------|
| `requireAuth()` | `error` | Verifica se há credenciais válidas |
| `requireProject()` | `(string, error)` | Retorna projectID do config, erro se não linkado |
| `requireServiceConfig()` | `(string, string, error)` | Retorna `(environmentID, serviceID)`, erro se não configurado |
| `resolveServiceContext(serviceRef)` | `(envID, serviceID, error)` | Se `serviceRef` vazio, fallback para `requireServiceConfig`; senão resolve via `ListServices` (match por ID/Name/Slug) usando `resolveEnvironmentID` |
| `resolveEnvironmentID(client, projectID)` | `(envID, error)` | Resolve envID na ordem: flag `-e` → linked envID → default name |
| `getEnvironment()` | `string` | Retorna nome de ambiente (flag > config > default) |
| `getProjectID()` | `string` | Retorna project ID (flag > config > vazio) |
| `getOutputFormat()` | `string` | Retorna formato (table \| json \| text) |

**Padrão `-s/--service`**: comandos que operam num service (`run`, `shell`, `variables`) aceitam `-s <name|slug|id>` para target ad-hoc, paridade com `railway -s <svc>`. Implementação: chamar `resolveServiceContext(flagValue)` em vez de `requireServiceConfig()`.

## API Client

### Uso

```go
client := api.NewClient()            // Usa credenciais salvas
```

### Métodos HTTP

```go
client.Get(path, &result)              // GET com JSON unmarshal
client.GetRaw(path) ([]byte, error)    // GET sem unmarshal (raw bytes, para logs/texto)
client.Post(path, body, &result)
client.Put(path, body, &result)
client.Delete(path)
```

`GetRaw` é usado quando a API retorna texto plano (ex: logs). Retorna `[]byte` em vez de fazer JSON unmarshal.

### Token refresh automático

O `doRequest` intercepta respostas 401 e tenta refresh via `/auth/refresh`. Se o refresh funcionar, a request original é repetida automaticamente.

### Error handling

```go
err := client.Get("/path", &result)
if apiErr, ok := err.(*api.APIError); ok {
    // apiErr.StatusCode, apiErr.Message
}
```

## Configuração — 3 Camadas

| Camada | Arquivo | Escopo |
|--------|---------|--------|
| Global | `~/.upuai/config.json` | Viper, defaults (apiUrl, environment, output) |
| Credenciais | `~/.upuai/credentials.json` | Token, refresh token, user info |
| Projeto | `.upuai/config.json` | projectId, projectName, environment, framework, environmentId, serviceId |

**Prioridade**: env var (`UPUAI_*`) > flag CLI > config projeto > config global > default

## UI — Componentes Disponíveis

### Output

```go
ui.PrintSuccess("Done!")           // ✓ Done!
ui.PrintError("Failed")            // ✗ Failed (para stderr)
ui.PrintWarning("Careful")         // ! Careful
ui.PrintInfo("Note")               // ℹ Note
ui.PrintKeyValue("Key", "val", "Key2", "val2")  // Pares alinhados
ui.PrintJSON(anyStruct)            // JSON indentado para stdout
ui.PrintBanner()                   // "Upuai Cloud" com tagline
```

### Interação

```go
value, err := ui.InputText("Title", "placeholder")
selected, err := ui.SelectOne("Choose:", []string{"a", "b"})
confirmed, err := ui.Confirm("Are you sure?")
```

### Spinner

```go
err := ui.RunWithSpinner("Loading...", func() error {
    // operação longa
    return nil
})
```

### Table

```go
table := ui.NewTable("Name", "Status", "URL")
table.AddRow("api", "running", "https://...")
table.Print()
```

### Output formats

```go
format := getOutputFormat()  // table | json | text
if format == ui.FormatJSON {
    ui.PrintJSON(data)
    return nil
}
```

### Cores e estilos (lipgloss)

- `ui.Carmesim`, `ui.Petroleo`, `ui.VerdeMusgo` — cores primárias
- `ui.Success`, `ui.Error`, `ui.Warning`, `ui.Info` — estilos de status
- `ui.Accent`, `ui.Muted`, `ui.Bold`, `ui.Dim` — estilos gerais
- `ui.StatusRunning`, `ui.StatusStopped`, `ui.StatusBuilding` — status de serviço

## Detecção de Frameworks

Para adicionar um novo framework, adicione ao slice `Frameworks` em `internal/detect/frameworks.go`:

```go
{
    Name:      "NuxtJS",
    Files:     []string{"nuxt.config.ts", "nuxt.config.js"},
    BuildCmd:  "npm run build",
    StartCmd:  "npm start",
    OutputDir: ".nuxt",
}
```

**Campos**: `Name`, `Files` (qualquer match = detectado), `BuildCmd`, `StartCmd`, `OutputDir` (opcional)

## File Watcher (`deploy --watch`)

- Usa fsnotify com debounce de 500ms
- Ignora: `.git`, `node_modules`, `.next`, `dist`, `build`, `.upuai`, `__pycache__`, `.venv`, `vendor`, `bin`
- Monitora recursivamente o diretório do projeto

## Convenções

### Nomenclatura
- **Arquivos**: kebab-case (`my-command.go`)
- **Pacotes**: snake_case ou single word (`config`, `detect`)
- **Funções públicas**: PascalCase (`NewClient`)
- **Funções privadas**: camelCase (`doRequest`)
- **Constantes**: camelCase para internas, PascalCase para exportadas

### Error handling
- Use `fmt.Errorf("context: %w", err)` para wrap
- Retorne errors do `RunE`, não faça `os.Exit` nos comandos
- Use `ui.PrintError()` apenas no root (já feito no `Execute()`)
- Para checar status code da API, type-assert para `*api.APIError`

### Flags
- Flags locais no `init()` do arquivo do comando
- Flags globais no `root.go`
- Use `--flag-name` (kebab-case)
- Variáveis de flag: `flagCamelCase` ou `commandFlagName`

### Confirmações
- Use `ui.Confirm()` antes de ações destrutivas (rollback, promote, delete, down)
- Respeite `--yes` (`flagYes`) para skip em CI

## Anti-Patterns

- **NÃO** use `fmt.Println` para erros — use `ui.PrintError` ou retorne error
- **NÃO** crie clients API em `init()` — crie dentro do `RunE`
- **NÃO** faça `os.Exit()` em comandos — retorne error para o root tratar
- **NÃO** acesse credenciais diretamente — use `config.NewCredentialStore()`
- **NÃO** ignore o output format — sempre cheque `getOutputFormat()` e suporte JSON
- **NÃO** hardcode URLs — use `config.GetAPIURL()`
- **NÃO** esqueça de registrar o comando no `init()` com `rootCmd.AddCommand()`

## Checklist para Novas Features

- [ ] Comando criado em `cmd/<name>.go` com `RunE`
- [ ] Registrado no `init()` com `rootCmd.AddCommand()`
- [ ] `requireAuth()` chamado se precisa de autenticação
- [ ] `requireProject()` chamado se precisa de projeto linkado
- [ ] `requireServiceConfig()` chamado se precisa de serviço (logs, restart, scale, etc)
- [ ] Spinner (`ui.RunWithSpinner`) para operações de API
- [ ] Suporte a `--output json` (`getOutputFormat()`)
- [ ] Confirmação (`ui.Confirm`) antes de ações destrutivas
- [ ] `--yes` respeitado para skip de confirmação
- [ ] Errors wrapped com contexto (`fmt.Errorf("ctx: %w", err)`)
- [ ] Endpoint adicionado em `internal/api/` (tipo + método no Client)
- [ ] Se subcomandos: registrar via `parentCmd.AddCommand()` + pai em `rootCmd.AddCommand()`
