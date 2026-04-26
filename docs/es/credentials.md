# Credenciales

[Read in English](../credentials.md)

<p align="center">
  <img src="../diagrams/credential-types.png" alt="Tipos de credenciales" width="850" />
</p>

## Tipos

gitbox soporta tres tipos de credencial:

- `gcm`: Git Credential Manager para equipos con navegador.
- `ssh`: claves SSH para servidores y entornos sin navegador.
- `token`: tokens personales para APIs, automatizacion y servidores Gitea o Forgejo.

## Reglas

Cada cuenta define `default_credential_type`. Cada repo puede sobrescribirlo con `credential_type`. gitbox no traduce esos valores porque forman parte de la configuracion y de la logica de ejecucion.

## Comprobacion

Uso:

```bash
gitbox credential verify <account>
gitbox doctor
```

`doctor` revisa herramientas externas como `git`, GCM, `ssh`, `tmux` y `wsl`.
