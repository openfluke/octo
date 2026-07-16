# Octo

**Octo** is Welvet’s interactive model shell — Lucy Bloom Rivers, rebuilt for Welvet.

| Piece | Owns |
|-------|------|
| **welvet** | Engine (layers, ENTITY runtime, generate) |
| **octo** (this) | Download · convert · quantize · run `.entity` |
| **w2a** | Layer test harness |

```bash
cd welvet/octo
go run .
```

Paste a Hugging Face repo (e.g. `HuggingFaceTB/SmolLM2-135M-Instruct`), download, convert to `.entity`, then run.

## Menu

| # | Action |
|---|--------|
| **1** | Run `.entity` model (chat / generate) |
| **2** | Download model (Hugging Face — paste any `org/name`) |
| **3** | Convert snapshot → `.entity` (Safetensors / GGUF) |
| **4** | List local hub snapshots + entities |
| **5** | Quantize / re-pack an existing `.entity` |
| **q** | Quit |

Nothing is hardcoded as “the” model — presets are prompts only.

## Paths

| Env | Default | Meaning |
|-----|---------|---------|
| `OCTO_HUB` | `./octo_hub` | HF-style hub root |
| `OCTO_ENTITIES` | `./octo_entities` | Converted `.entity` files |
| `HUGGING_FACE_HUB_TOKEN` | — | Optional for gated repos |

## Status (honest)

- ✅ Interactive menu, `go run .`
- ✅ HF download by pasted `org/name` (API tree → files)
- ✅ Convert Safetensors → packed `.entity` (FormatNone / Float32)
- ✅ Run / chat (greedy, host ALU) for Llama-style models e.g. SmolLM2-135M
- 🚧 Quantize / Q4 re-pack, GGUF import
- 🚧 WebGPU generate, Top-K / temperature sampling

SmolLM2-135M Instruct is a good first paste target (~270 MB safetensors → ~513 MB FormatNone `.entity`).
