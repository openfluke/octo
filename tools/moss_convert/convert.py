#!/usr/bin/env python3
"""Convert MOSS-TTS-Nano pytorch_model.bin → Welvet-friendly safetensors + vocab dump.

Usage:
  python convert.py /path/to/MOSS-TTS-Nano-100M /path/to/out_dir

Requires: torch, safetensors, sentencepiece

Output layout (drop under octo_hub/.../manual-download/ or host as your own HF repo):
  config.json              (copied)
  tokenizer.model          (copied)
  tokenizer_config.json    (copied)
  model.safetensors        (converted dense F32 weights)
  vocab_pieces.json        (SentencePiece pieces for Go encode)
  README_WELVET.md         (layout notes)
"""
from __future__ import annotations

import json
import shutil
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 2
    src = Path(sys.argv[1]).resolve()
    dst = Path(sys.argv[2]).resolve()
    dst.mkdir(parents=True, exist_ok=True)

    bin_path = src / "pytorch_model.bin"
    if not bin_path.is_file():
        # already safetensors?
        st = src / "model.safetensors"
        if st.is_file():
            print(f"already have {st}; copying snapshot files")
            for name in ("config.json", "tokenizer.model", "tokenizer_config.json", "special_tokens_map.json"):
                p = src / name
                if p.is_file():
                    shutil.copy2(p, dst / name)
            shutil.copy2(st, dst / "model.safetensors")
            dump_vocab(src / "tokenizer.model", dst / "vocab_pieces.json")
            write_readme(dst)
            return 0
        print(f"missing {bin_path}", file=sys.stderr)
        return 1

    import torch
    from safetensors.torch import save_file

    print(f"loading {bin_path} …")
    state = torch.load(bin_path, map_location="cpu", weights_only=True)
    out: dict[str, torch.Tensor] = {}
    for k, v in state.items():
        if not torch.is_floating_point(v):
            # skip non-float buffers if any
            if v.dtype in (torch.int64, torch.int32, torch.bool):
                continue
        t = v.detach().contiguous().float().cpu()
        out[k] = t
    print(f"writing {len(out)} tensors → model.safetensors")
    save_file(out, str(dst / "model.safetensors"))

    for name in ("config.json", "tokenizer.model", "tokenizer_config.json", "special_tokens_map.json", "prompting.py"):
        p = src / name
        if p.is_file() and p.resolve() != (dst / name).resolve():
            shutil.copy2(p, dst / name)

    tok = dst / "tokenizer.model"
    if not tok.is_file():
        tok = src / "tokenizer.model"
    dump_vocab(tok, dst / "vocab_pieces.json")
    write_readme(dst)
    print(f"done → {dst}")
    return 0


def dump_vocab(model_path: Path, out_path: Path) -> None:
    import sentencepiece as spm

    sp = spm.SentencePieceProcessor()
    sp.Load(str(model_path))
    pieces = []
    for i in range(sp.get_piece_size()):
        pieces.append({"id": i, "piece": sp.id_to_piece(i), "score": float(sp.get_score(i))})
    meta = {
        "unk_id": int(sp.unk_id()),
        "bos_id": int(sp.bos_id()),
        "eos_id": int(sp.eos_id()),
        "pad_id": int(sp.pad_id()) if hasattr(sp, "pad_id") else -1,
        "pieces": pieces,
    }
    out_path.write_text(json.dumps(meta), encoding="utf-8")
    print(f"vocab {len(pieces)} pieces → {out_path}")


def write_readme(dst: Path) -> None:
    (dst / "README_WELVET.md").write_text(
        """# MOSS-TTS-Nano Welvet snapshot

Converted for native Welvet/Octo (`welvet/mosstts`).

## Layout

| File | Role |
|------|------|
| `model.safetensors` | Dense F32 AR weights (from `pytorch_model.bin`) |
| `config.json` | MossTTSNanoConfig |
| `tokenizer.model` | SentencePiece |
| `vocab_pieces.json` | Pieces+scores for Go SP encode |
| `audio_tokenizer/` | Optional: symlink/copy of `MOSS-Audio-Tokenizer-Nano` |

Place this directory at:

```
octo_hub/models--YOUR--REPO/snapshots/manual-download/
```

Also place the audio tokenizer next to it or under `audio_tokenizer/`:

```
octo_hub/models--OpenMOSS-Team--MOSS-Audio-Tokenizer-Nano/snapshots/manual-download/
```

Octo menu **[9] Generate speech** looks for both.
""",
        encoding="utf-8",
    )


if __name__ == "__main__":
    raise SystemExit(main())
