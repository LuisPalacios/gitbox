# Notas de la próxima versión

## Transferencias de repositorios y limpieza de ramas

Gitbox retira **Mover repositorio…**. Transfiere el repositorio con la función del proveedor o copia las refs manualmente con `git push --mirror` desde un clon de espejo a un destino vacío. Consulta la [guía de transferencias](gui-guide.md#transferir-un-repositorio-entre-cuentas-o-proveedores).

**Limpiar ramas** elimina únicamente ramas fusionadas. Para revisar las ramas con el upstream desaparecido o con cambios equivalentes a un squash, abre el clon en tu cliente Git. Los espejos siguen funcionando en los servidores de los proveedores.
