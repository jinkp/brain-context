# brain-context — Backlog

Última actualización: 2026-10-09

---

## ✅ Completado

| Change | Estado | Commit |
|--------|--------|--------|
| `brain-context-secure-invite-search` | ✅ Completo (Phases 1-4) | `a83988c` |
| `brain-context-okf-knowledge-layer` V1 | ✅ Completo (55 tasks) | `2541345` |

---

## 🔜 Siguiente: Release v0.8.0

- [ ] Tag `v0.8.0` en GitHub
- [ ] Renovar API key OpenAI (la actual expiró)
- [ ] Deploy API a Cloud Run con la imagen actualizada
- [ ] Actualizar install scripts (`install.sh`, `install.ps1`) con nuevo binario

---

## 📋 Backlog activo

### OKF V2 — Server sync y shared bundles

**Motivación**: En V1 el OKF es local-only. Los developers que hacen `brain join` no ven el conocimiento curado del Tech Lead. V2 resuelve esto.

**Scope**:
- `POST /api/okf/sync` — sube bundle al servidor
- `okf_concepts` table en Postgres con multi-tenant RLS
- `mcp_read_key` accede a OKF shared del proyecto
- Múltiples bundles por proyecto (`[]OKFBundle` en config)
- `okf_global` en config: bundles que sirven a todos los proyectos

**Prerequisito**: Ninguno. Puede arrancar cuando se quiera.

---

### brain-context-local-cache

**Motivación**: Reducir latencia y costo cacheando queries frecuentes localmente.

**Estado**: No iniciado. Ver `sdd/brain-context-local-cache/tasks.md` para el detalle.

**Nota**: Evaluar si sigue siendo necesario después de OKF V2 (OKF ya reduce tokens para queries de intención).

**Prerequisito**: Recomendado después de OKF V2.

---

## 🔍 Ideas / Exploraciones futuras

| Idea | Descripción |
|------|-------------|
| `brain okf reindex` | Comando explícito para re-indexar sin re-registrar |
| `brain help <cmd>` | Ayuda detallada por subcomando (para uso con LLM) |
| Adaptive threshold | Calibrar threshold OKF automáticamente según tamaño del bundle |
| OKF auto-update | Post-hook en `brain update` que regenera drafts automáticamente |
| Solution + OKF | Fan-out de solution que incluye OKF de cada proyecto |
| `brain okf export` | Exportar bundle a formato compartible (zip, tarball) |

---

## 📊 Estado técnico

```
Tests: 124 passing / 0 failing
Go:    1.26+
DB:    PostgreSQL + pgvector (Cloud Run)
OKF:   SQLite FTS5 local (~/.brain/okf/)
```

### Sandbox local verificado

| Proyecto | Tipo | Bundle | Estado |
|----------|------|--------|--------|
| `brain-context-openai` | Código + OKF | `brain-docs/` | ✅ Activo |
| `organization` | Local-only OKF | `organization-docs/` | ✅ Activo |
