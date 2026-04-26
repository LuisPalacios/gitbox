<p align="center">
  <img src="assets/logo.svg" width="128" alt="gitbox">
</p>

<h1 align="center">Gitbox</h1>

<p align="center">
  <strong>Cuentas y clones, nada mas.</strong><br>
  <em>gitbox nunca anade commits, hace push ni modifica tus arboles de trabajo.</em>
</p>

[Read in English](readme.md)

---

## Por que uso gitbox

Gestiono varias cuentas Git en GitHub, GitLab, Gitea, Forgejo y Bitbucket. El problema se repite: las credenciales se mezclan, los clones acaban con una identidad incorrecta y cada maquina nueva exige configurar todo otra vez.

Construyo gitbox para resolver eso. Defino mis cuentas, descubro repositorios, clono con la identidad correcta y veo el estado de sincronizacion desde CLI, TUI o GUI.

## Instalar con el script bootstrap

Para macOS, Linux o Windows con Git Bash:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

El instalador coloca los binarios en `~/bin/`. En Linux tambien registra la GUI en el menu de aplicaciones, salvo que use `--no-desktop`.

## Que hace

- Gestiona varias cuentas por proveedor con credenciales separadas.
- Descubre repositorios mediante las APIs de los proveedores.
- Clona cada repo en la carpeta y con la identidad correctas.
- Muestra si cada repo esta limpio, atrasado, adelantado, divergente o sin remoto.
- Ejecuta pulls seguros con fast-forward.
- Configura mirrors entre proveedores cuando la API lo permite.
- Abre clones en navegador, explorador, terminal, editor o harness de IA.

## Guias

- [Guia CLI](docs/es/cli-guide.md)
- [Guia GUI](docs/es/gui-guide.md)
- [Credenciales](docs/es/credentials.md)
- [Referencia](docs/es/reference.md)
- [Completado de shell](docs/es/completion.md)
