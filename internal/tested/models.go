package tested

// Model is a known-good HF repo for Octo download → convert → run.
type Model struct {
	Repo   string // org/name
	Title  string // short label
	Note   string // one-line capability / VRAM hint
	// FormatHint is empty to auto-detect after download (MLX 1-bit → BinaryPacked, else Q4_0).
	FormatHint string
}

// Models are entities that have been exercised on the Welvet/Octo path.
var Models = []Model{
	{
		Repo:       "HuggingFaceTB/SmolLM2-135M-Instruct",
		Title:      "SmolLM2-135M",
		Note:       "Q4_0 Lucy fuse — tiny, any GPU/CPU",
		FormatHint: "q4_0",
	},
	{
		Repo:       "prism-ml/Bonsai-1.7B-mlx-1bit",
		Title:      "Bonsai-1.7B",
		Note:       "BinaryG128 dense — fits 4GB+; use gpu_fuse",
		FormatHint: "binarypacked",
	},
	{
		Repo:       "prism-ml/Bonsai-4B-mlx-1bit",
		Title:      "Bonsai-4B",
		Note:       "BinaryG128 dense — fits 6GB; use gpu_fuse",
		FormatHint: "binarypacked",
	},
	{
		Repo:       "prism-ml/Bonsai-8B-mlx-1bit",
		Title:      "Bonsai-8B",
		Note:       "BinaryG128 dense — fits 6GB; use gpu_fuse",
		FormatHint: "binarypacked",
	},
	{
		Repo:       "prism-ml/Bonsai-27B-mlx-1bit",
		Title:      "Bonsai-27B hybrid",
		Note:       "BinaryG128 GDN hybrid — needs ~8GB+ VRAM for gpu_fuse",
		FormatHint: "binarypacked",
	},
}
