# Octo templates

JSON recipes for `./octo bench <name>`. One file per model / experiment — **not** hardcoded to SmolLM2.

## Schema

```json
{
  "name": "my-bench",
  "model": { "repo": "org/name", "download": true },
  "quantize": "all",
  "system_prompt": "You are a helpful assistant.",
  "messages": ["Say hi."],
  "max_tokens": 16,
  "profiles": ["simd_fuse", "gpu_fuse"],
  "skip_convert_if_exists": true
}
```

| Field | Notes |
|-------|--------|
| `model.repo` | Hugging Face `org/name` |
| `quantize` | `"all"`, one name (`"Q4_0"`), or `["Q4_0","Q8_0"]` |
| `profiles` | `"all"` or list (`simd_fuse`, `gpu_fuse`, `simd_mc`, …) |
| `skip_convert_if_exists` | skip pack when `.entity` already on disk |

## Examples

```bash
./octo bench smol2_135m_fuse          # by name under templates/
./octo bench templates/foo.json       # by path
```

Drop new files in `templates/` — menu [6] and `octo help` list them automatically.
