# Architecture — C4 Diagrams

C4 architecture diagrams for gitbox rendered in Mermaid flowchart syntax.

## Level 1 — System Context

How gitbox fits into the broader environment: actors, the system itself, and
every external dependency it touches.

```mermaid
flowchart TB
    %% Actors
    dev([Developer\npower user / CI])
    desktop([Desktop User])

    %% Central system — github.com/LuisPalacios/gitbox
    gitbox[gitbox\nGo monorepo\nCLI + GUI]

    %% External systems — Provider APIs
    github{{GitHub API\nREST v3}}
    gitlab{{GitLab API\nREST v4}}
    gitea{{Gitea / Forgejo API\nREST /api/v1}}
    bitbucket{{Bitbucket API\nREST v2}}

    %% External systems — Local
    oscred{{OS Credential Store\nWin Cred Manager /\nmacOS Keychain /\nLinux Secret Service}}
    gitbin{{Local Git Binary\nsystem git via os/exec}}
    fs{{Local Filesystem\n~/00.git/ repo tree}}
    ssh{{SSH Agent +\n~/.ssh/config}}

    %% Relationships — actors to system
    dev -->|Cobra CLI commands| gitbox
    desktop -->|Wails GUI WebView| gitbox

    %% Relationships — system to external
    gitbox -->|REST API calls\nnet/http| github
    gitbox -->|REST API calls\nnet/http| gitlab
    gitbox -->|REST API calls\nnet/http| gitea
    gitbox -->|REST API calls\nnet/http| bitbucket
    gitbox -->|go-keyring\nstore/retrieve tokens| oscred
    gitbox -->|os/exec\nclone, pull, fetch,\nstatus, config| gitbin
    gitbox -->|read/write\nconfig + repos| fs
    gitbox -->|SSH key management +\nconnection tests| ssh
```

## Level 2 — Container

Internal containers, the shared library packages, data stores, and how they
connect to external systems.

```mermaid
flowchart TB
    %% Actors
    dev([Developer\npower user / CI])
    desktop([Desktop User])

    %% Containers
    %% github.com/LuisPalacios/gitbox/cmd/cli
    cli[gitbox\nCLI — Go + Cobra]
    %% github.com/LuisPalacios/gitbox/cmd/gui
    gui[gitbox\nGUI — Go + Wails v2 runtime]
    %% github.com/LuisPalacios/gitbox/cmd/gui/frontend
    svelte[Svelte Frontend\nembedded WebView]

    %% Shared library — github.com/LuisPalacios/gitbox/pkg/*
    subgraph pkg [Shared Library — pkg/]
        direction TB
        %% github.com/LuisPalacios/gitbox/pkg/config
        pkgConfig[config\nLoad / Save / Migrate / CRUD]
        %% github.com/LuisPalacios/gitbox/pkg/credential
        pkgCred[credential\nToken, GCM, SSH\nsetup + validation]
        %% github.com/LuisPalacios/gitbox/pkg/provider
        pkgProvider[provider\nGitHub, GitLab,\nGitea/Forgejo, Bitbucket\nListRepos, CreateRepo,\nOrgLister, PushMirror,\nPullMirror, RepoInfo]
        %% github.com/LuisPalacios/gitbox/pkg/git
        pkgGit[git\nClone, Pull, Fetch,\nStatus, Config — os/exec]
        %% github.com/LuisPalacios/gitbox/pkg/status
        pkgStatus[status\nSync state checking\nClean, Dirty, Behind...]
        %% github.com/LuisPalacios/gitbox/pkg/mirror
        pkgMirror[mirror\nPush/Pull mirror setup,\nstatus, discovery]
        %% github.com/LuisPalacios/gitbox/pkg/identity
        pkgIdentity[identity\nGit author identity\nmanagement]
    end

    %% Data stores
    configFile[(~/.config/gitbox/\ngitbox.json)]
    credFiles[(~/.config/gitbox/\ncredentials/)]
    sshKeys[(~/.ssh/\nkeys + config)]

    %% External systems
    providerAPIs{{Provider APIs\nGitHub, GitLab,\nGitea/Forgejo, Bitbucket}}
    osKeyring{{OS Credential Store\nkeyring}}
    systemGit{{System git binary}}

    %% Actor to container
    dev -->|CLI commands| cli
    desktop -->|desktop app| gui
    gui <-->|Wails bindings\nwindow.go.main.App.*| svelte

    %% Container to pkg
    cli --> pkg
    gui --> pkg

    %% Intra-pkg relationships
    pkgCred -->|resolves tokens for| pkgProvider
    pkgMirror -->|uses| pkgProvider
    pkgMirror -->|uses| pkgCred
    pkgStatus -->|uses| pkgGit

    %% pkg to data stores
    pkgConfig -->|read/write JSON| configFile
    pkgCred -->|read/write token files| credFiles
    pkgCred -->|manage SSH keys +\nconfig entries| sshKeys

    %% pkg to external systems
    pkgProvider -->|REST API calls\nnet/http| providerAPIs
    pkgGit -->|os/exec| systemGit
    pkgCred -->|go-keyring| osKeyring
```
