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
| **6** | Run benchmark template |
| **7** | Tested models (download + convert) |
| **8** | Generate image (Flux2 / Bonsai) |
| **9** | Generate speech (MOSS-TTS-Nano) |
| **q** | Quit |

```bash
./octo image "a red bicycle"   # → octo_outputs/*.png (GPU)
./octo speak "Hello."          # → octo_outputs/*.wav (CPU; see welvet/mosstts/README.md)
```

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
- ✅ Convert Safetensors → packed `.entity` (FP32 or baked quant — Q4_0 default)
- ✅ Run / chat (greedy, host ALU) for Llama-style models e.g. SmolLM2-135M
- ✅ Convert bakes **all k-quants / IQ / BitNet** into `.entity` at save (default Q4_0); tied LM head gets `transformer.lm_head.packed` (+H)
- ✅ Run settings: `simd_fuse` / `gpu_fuse` use entity baked quant when present (no runtime repack); format picker only for FP32 entities
- 🚧 Quantize / Q4 re-pack, GGUF import
- 🚧 Full fused GPU forward (Lucy-style); `gpu` today = dense projections + LM head on device, attn/norm/embed on host
- 🚧 Top-K / temperature sampling

SmolLM2-135M: ~270 MB safetensors → ~189 MB Q4_0 `.entity` (Lucy-sized) or ~513 MB FP32.
