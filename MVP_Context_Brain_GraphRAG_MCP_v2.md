# MVP — Cerebro de Contexto para Proyectos con MCP, Go y PostgreSQL

## 1. Objetivo

Diseñar un sistema que permita a un cliente como Codex, un IDE o un agente LLM consultar el contexto relevante de un proyecto sin tener que escanear todos los archivos cada vez.

El objetivo principal es:

- Aumentar la velocidad de respuesta.
- Reducir consumo de tokens.
- Mejorar la precisión del contexto entregado al LLM.
- Crear una base reutilizable para múltiples proyectos.
- Exponer APIs públicas y un servidor MCP para integrarse con herramientas de desarrollo.

## 2. Problema que resuelve

Actualmente, cuando un LLM necesita responder preguntas sobre un proyecto, normalmente debe leer muchos archivos o recibir demasiado contexto.

Ejemplo:

> ¿Cómo funciona el login?

Sin un sistema de contexto, el LLM podría intentar revisar controladores, servicios, repositorios, configuración, entidades, rutas y pruebas.

Esto genera:

- Mucho consumo de tokens.
- Respuestas lentas.
- Riesgo de omitir archivos importantes.
- Contexto duplicado o irrelevante.
- Mayor costo operativo.

## 3. Idea del MVP

Crear un “cerebro de contexto” que indexe el proyecto una vez y después permita hacer búsquedas inteligentes.

Flujo deseado:

```text
Usuario en Codex / IDE
   ↓
Pregunta: "¿Cómo funciona el login?"
   ↓
Codex consulta al MCP Server
   ↓
MCP Server consulta la Context API
   ↓
La API busca en PostgreSQL + pgvector
   ↓
Devuelve archivos, funciones, endpoints y relaciones relevantes
   ↓
Codex responde usando solo el contexto necesario
```

## 4. Alcance del MVP

El MVP no busca resolver todo GraphRAG desde el inicio.

Debe enfocarse en:

- Indexar un repositorio.
- Detectar archivos relevantes.
- Dividir archivos en chunks inteligentes.
- Generar embeddings.
- Guardar metadata y relaciones básicas.
- Permitir búsquedas por pregunta.
- Exponer resultados vía API.
- Exponer herramientas vía MCP.

## 5. Componentes principales

### 5.1 Context API

API principal desarrollada en Go.

Responsabilidades:

- Registrar proyectos.
- Lanzar indexaciones.
- Consultar contexto.
- Consultar archivos relacionados.
- Administrar proyectos, usuarios y permisos.
- Exponer endpoints públicos.

### 5.2 MCP Server

Servidor MCP que conecta herramientas como Codex, IDEs o agentes LLM con el cerebro de contexto.

Herramientas iniciales:

```text
search_project_context
get_file_summary
get_related_files
explain_flow
find_impact
```

### 5.3 Worker Indexador

Servicio encargado de procesar repositorios.

Responsabilidades:

- Clonar o leer el proyecto.
- Detectar archivos modificados.
- Calcular hash por archivo.
- Parsear archivos.
- Crear chunks.
- Generar embeddings.
- Guardar metadata.
- Actualizar relaciones.

### 5.4 PostgreSQL + pgvector

Base principal del MVP.

Responsabilidades:

- Usuarios.
- Proyectos.
- Archivos.
- Chunks.
- Embeddings.
- Relaciones básicas.
- Jobs de indexación.
- Auditoría.

### 5.5 Storage

Puede ser S3 o almacenamiento local en MVP.

Responsabilidades:

- Guardar snapshots.
- Guardar dumps de indexación.
- Guardar archivos procesados si aplica.

### 5.6 Cola de trabajos

Puede iniciar con Redis, SQS o incluso una tabla de jobs en PostgreSQL.

Responsabilidades:

- Encolar indexaciones.
- Reintentar procesos fallidos.
- Separar la API del procesamiento pesado.

## 6. Arquitectura inicial

```text
Codex / IDE / Cliente
        ↓
    MCP Server
        ↓
    Context API Go
        ↓
 PostgreSQL + pgvector
        ↓
 Indexer Worker
        ↓
 Git Repo / Archivos
```

## 7. Arquitectura futura con grafo

Cuando el MVP madure, se puede agregar Neo4j.

```text
Codex / IDE
   ↓
MCP Server
   ↓
Context API Go
   ↓
Retriever Service
   ↓
PostgreSQL + pgvector
   ↓
Neo4j
   ↓
LLM
```

Uso de Neo4j:

- Relaciones complejas.
- Impact analysis.
- Dependencias entre módulos.
- Flujo de endpoints.
- Relación entre servicios, tablas, clases y funciones.

## 8. Modelo conceptual de datos

### Entidades iniciales

```text
Project
File
Chunk
Symbol
Endpoint
Dependency
Embedding
IndexJob
```

### Relaciones iniciales

```text
Project HAS_FILE File
File HAS_CHUNK Chunk
File DEFINES Symbol
Symbol CALLS Symbol
Endpoint HANDLED_BY Symbol
File DEPENDS_ON File
Chunk MENTIONS Symbol
```

## 9. Ejemplo de contexto esperado

Pregunta:

```text
¿Cómo funciona el login?
```

Respuesta interna del sistema:

```json
{
  "query": "como funciona el login",
  "projectId": "sr-cloud-api",
  "summary": "El login inicia en AuthController, valida credenciales mediante AuthService, consulta usuarios y genera un JWT.",
  "relevantFiles": [
    "Controllers/AuthController.cs",
    "Services/AuthService.cs",
    "Repositories/UserRepository.cs",
    "Config/JwtSettings.cs"
  ],
  "relationships": [
    "AuthController.Login -> AuthService.Login",
    "AuthService.Login -> UserRepository.GetByEmail",
    "AuthService.Login -> JwtTokenGenerator.Generate"
  ],
  "chunks": [
    {
      "file": "Controllers/AuthController.cs",
      "symbol": "Login",
      "reason": "Endpoint principal de autenticación"
    },
    {
      "file": "Services/AuthService.cs",
      "symbol": "Login",
      "reason": "Contiene la lógica de validación y creación del token"
    }
  ]
}
```

## 10. Endpoints iniciales de la API

### Crear proyecto

```http
POST /api/projects
```

Body:

```json
{
  "name": "sr-cloud-api",
  "repositoryUrl": "https://bitbucket.org/company/project.git",
  "defaultBranch": "develop"
}
```

### Lanzar indexación

```http
POST /api/projects/{projectId}/index
```

### Consultar contexto

```http
POST /api/projects/{projectId}/context/search
```

Body:

```json
{
  "query": "como funciona el login",
  "maxChunks": 8,
  "includeRelationships": true
}
```

### Obtener resumen de archivo

```http
GET /api/projects/{projectId}/files/summary?path=Services/AuthService.cs
```

### Obtener archivos relacionados

```http
GET /api/projects/{projectId}/files/related?path=Services/AuthService.cs
```

### Análisis de impacto

```http
POST /api/projects/{projectId}/impact
```

Body:

```json
{
  "entity": "Users table"
}
```

## 11. Herramientas MCP iniciales

### search_project_context

Busca el contexto más relevante para una pregunta.

Input:

```json
{
  "projectId": "sr-cloud-api",
  "query": "como funciona el login"
}
```

Output:

```json
{
  "summary": "El login inicia en AuthController...",
  "files": [
    "Controllers/AuthController.cs",
    "Services/AuthService.cs"
  ],
  "chunks": [],
  "relationships": []
}
```

### get_file_summary

Devuelve el resumen de un archivo.

### get_related_files

Devuelve archivos relacionados por dependencias, llamadas o menciones.

### explain_flow

Explica un flujo funcional como login, facturación, creación de orden o pagos.

### find_impact

Busca impacto probable de modificar una entidad, archivo, tabla, endpoint o servicio.

## 12. Tecnologías recomendadas

### MVP

```text
Lenguaje: Go
API: Gin, Fiber o Echo
Base de datos: PostgreSQL
Vector store: pgvector
Worker: Go worker
Cola: PostgreSQL jobs, Redis o SQS
Storage: Local o S3
Auth: JWT/API Keys
MCP: servidor MCP propio
```

### Futuro

```text
Neo4j para grafo avanzado
Qdrant si pgvector queda corto
SQS para colas productivas
EKS o ECS Fargate para despliegue
OpenTelemetry para trazabilidad
```

## 13. Infraestructura recomendada

### Desarrollo local

```text
Docker Compose
 ├── context-api
 ├── mcp-server
 ├── indexer-worker
 ├── postgres + pgvector
 └── redis opcional
```

### Producción inicial en AWS

```text
ECS Fargate o App Runner
 ├── context-api
 ├── mcp-server
 └── indexer-worker

RDS PostgreSQL + pgvector
S3
SQS
Secrets Manager
CloudWatch
```

### Producción avanzada

```text
EKS
 ├── pod: context-api
 ├── pod: mcp-server
 ├── pod: indexer-worker
 ├── pod: retriever-service
 └── ingress

RDS PostgreSQL + pgvector
Neo4j
S3
SQS
Secrets Manager
CloudWatch / OpenTelemetry
```

## 14. Estrategia de indexación

### Primera indexación

```text
1. Leer repositorio.
2. Ignorar archivos innecesarios.
3. Detectar lenguaje y tipo de archivo.
4. Parsear clases, funciones, endpoints y configuración.
5. Crear chunks.
6. Generar embeddings.
7. Guardar metadata.
8. Guardar relaciones básicas.
```

### Indexación incremental

```text
1. Detectar archivos cambiados por hash o Git diff.
2. Reprocesar solo archivos modificados.
3. Eliminar chunks antiguos.
4. Insertar chunks nuevos.
5. Actualizar embeddings.
6. Actualizar relaciones.
```

## 15. Archivos a ignorar inicialmente

```text
node_modules/
bin/
obj/
dist/
build/
.git/
.vscode/
.idea/
coverage/
*.min.js
*.lock
*.png
*.jpg
*.zip
*.pdf
```

## 16. Chunks inteligentes

No partir archivos solo por cantidad de caracteres.

Mejor dividir por:

```text
Clase
Función
Método
Endpoint
Componente
Config relevante
Consulta SQL
Migración
DTO / modelo
```

Cada chunk debe tener metadata:

```json
{
  "projectId": "sr-cloud-api",
  "filePath": "Services/AuthService.cs",
  "language": "csharp",
  "symbolName": "Login",
  "symbolType": "method",
  "startLine": 20,
  "endLine": 85,
  "hash": "abc123"
}
```

## 17. Seguridad

El API pública debe tener:

- Autenticación.
- API keys por cliente o proyecto.
- Rate limit.
- Control por tenant.
- Permisos por proyecto.
- Logs de auditoría.
- Cifrado de secretos.

No se debe exponer directamente:

```text
PostgreSQL
Neo4j
Storage interno
Tokens de repositorios
Credenciales de proveedores LLM
```

## 18. Multi-tenant

Desde el MVP conviene contemplar tenant.

Modelo sugerido:

```text
Tenant
 └── Projects
      └── Files
           └── Chunks
```

Cada consulta debe filtrar por:

```text
tenant_id
project_id
user_id / api_key
```

## 19. Flujo de búsqueda

```text
1. Recibir pregunta.
2. Generar embedding de la pregunta.
3. Buscar chunks similares en pgvector.
4. Aplicar filtros por proyecto y tenant.
5. Reordenar por relevancia.
6. Agregar relaciones básicas.
7. Construir contexto compacto.
8. Responder al MCP o cliente.
```

## 20. Criterios de éxito del MVP

El MVP será exitoso si logra:

- Responder preguntas de un proyecto sin escanear todo el repositorio.
- Reducir contexto enviado al LLM.
- Identificar archivos relevantes para una pregunta.
- Responder en pocos segundos.
- Reindexar solo archivos modificados.
- Integrarse con un cliente vía MCP o API.

## 21. Fases propuestas

### Fase 1 — Base local

- Docker Compose.
- Go API.
- PostgreSQL + pgvector.
- Worker básico.
- Indexación de archivos.
- Búsqueda por embeddings.

### Fase 2 — MCP

- MCP Server.
- Tool `search_project_context`.
- Tool `get_file_summary`.
- Integración con cliente/IDE.

### Fase 3 — Relaciones básicas

- Detectar imports.
- Detectar llamadas entre archivos.
- Detectar endpoints.
- Detectar tablas o queries SQL.
- Crear búsqueda con relaciones.

### Fase 4 — Producción inicial

- Despliegue en ECS Fargate o App Runner.
- RDS PostgreSQL.
- S3.
- SQS.
- Secrets Manager.
- Logs en CloudWatch.

### Fase 5 — Grafo avanzado

- Agregar Neo4j.
- Impact analysis.
- Flujo de negocio.
- Dependencias entre módulos.
- GraphRAG completo.

## 22. Decisión recomendada

No iniciar directamente con EKS ni Neo4j.

Recomendación:

```text
MVP:
Go + PostgreSQL + pgvector + MCP + Worker

Después:
Neo4j + EKS + GraphRAG avanzado
```

Esto permite validar rápido la idea, reducir complejidad y aprender qué relaciones realmente aportan valor.

## 23. Nombre tentativo del sistema

Opciones:

```text
Context Brain
Project Brain
CodeGraph Context
DevContext Hub
GraphContext API
```

## 24. Resumen ejecutivo

Este MVP propone construir un sistema de contexto inteligente para proyectos de software.

El sistema indexa repositorios, genera embeddings, guarda metadata y relaciones básicas, y expone una API/MCP para que herramientas como Codex puedan consultar contexto relevante antes de responder.

El beneficio principal es evitar que el LLM escanee todos los archivos, reduciendo tokens, mejorando velocidad y aumentando precisión.

La primera versión debe construirse con Go, PostgreSQL + pgvector y un worker de indexación. Neo4j y EKS deben considerarse fases posteriores cuando el producto ya tenga tracción y se requieran recorridos complejos de grafo.


---

# Flujo de Indexación Incremental (`init` / `update`)

## Objetivo

El sistema debe evitar reindexar el proyecto completo cada vez que exista una consulta o un cambio de código.

La estrategia recomendada es:

```text
init → snapshot → update incremental → ask
```

Esto permite:

- Reducir consumo de tokens
- Mejorar velocidad de recuperación
- Disminuir costo de embeddings
- Mantener contexto actualizado
- Evitar escaneo completo del repositorio

---

## Flujo General

```text
1. init
   Escanea todo el proyecto por primera vez

2. snapshot
   Guarda estado actual: archivos, hashes, commits, chunks

3. update
   Solo procesa lo que cambió

4. query
   Codex/MCP consulta el contexto optimizado
```

---

## Comandos Conceptuales

### CLI

```bash
context-brain init --project ./mi-api
context-brain update --project ./mi-api
context-brain ask "cómo funciona el login"
```

### API

```http
POST /projects/{id}/init
POST /projects/{id}/update
POST /projects/{id}/ask
```

---

## ¿Qué hace `init`?

El comando `init` realiza el primer escaneo completo del proyecto.

### Procesos

```text
- Lee archivos permitidos
- Ignora node_modules, bin, obj, dist, .git, logs
- Calcula hash por archivo
- Detecta lenguaje
- Extrae funciones, clases y endpoints
- Crea chunks
- Genera embeddings
- Guarda relaciones
- Crea resumen por archivo
- Crea resumen por módulo
```

---

## ¿Qué guarda el sistema?

### Metadata de archivos

```text
project_id
file_path
file_hash
last_commit
language
indexed_at
```

### Información contextual

```text
entities
chunks
embeddings
relationships
summary
```

---

## ¿Qué hace `update`?

El comando `update` compara el estado actual contra el snapshot previo.

### Estrategia

```text
Si archivo nuevo:
   indexar

Si archivo modificado:
   borrar chunks anteriores
   reindexar solo ese archivo

Si archivo eliminado:
   eliminar del índice

Si archivo sin cambios:
   no tocar
```

---

## Estrategia de Optimización

La recomendación es manejar 3 niveles de hash:

```text
Nivel 1: hash del archivo
Nivel 2: hash del chunk
Nivel 3: hash de entidad
```

### Ejemplo

```text
AuthService.cs cambió
   ↓
Revisar funciones internas
   ↓
Solo cambió Login()
   ↓
Reindexar chunk de Login()
   ↓
No tocar Register(), Logout(), RefreshToken()
```

---

## Estructura Recomendada en Go

```text
/cmd
  /context-brain
    main.go

/internal
  /scanner
  /parser
  /chunker
  /embedder
  /indexer
  /retriever
  /snapshot
  /api
  /mcp
```

---

## Modelo Inicial de Tablas

### project_files

```sql
project_files
- id
- project_id
- path
- language
- hash
- last_commit
- indexed_at
- status
```

### chunks

```sql
chunks
- id
- file_id
- entity_name
- chunk_type
- content
- summary
- content_hash
- embedding
```

### relationships

```sql
relationships
- source_entity
- target_entity
- relation_type
```

---

## Resultado Esperado

El sistema debe permitir que Codex/MCP consulte únicamente el contexto mínimo y relevante del proyecto sin necesidad de escanear el repositorio completo.

Ejemplo:

```text
Usuario:
"¿Cómo funciona el login?"

Retriever:
- AuthController
- AuthService
- UserRepository
- JwtConfig
- PasswordHasher
- Tabla Users

LLM:
Responde usando únicamente esos fragmentos relevantes
```
