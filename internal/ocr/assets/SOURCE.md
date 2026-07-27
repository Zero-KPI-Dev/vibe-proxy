# Built-in OCR assets

`chi_sim.traineddata` is the simplified Chinese fast model from
[`tesseract-ocr/tessdata_fast`](https://github.com/tesseract-ocr/tessdata_fast).
The model also recognizes the Latin characters used in common English
screenshots, allowing vibe-proxy to ship one language file instead of separate
Chinese and English models.

- Source revision: `main`, downloaded 2026-07-27
- SHA-256: `a5fcb6f0db1e1d6d8522f39db4e848f05984669172e584e8d76b6b3141e1f730`
- License: Apache-2.0; see `LICENSE.tessdata_fast`

The model is embedded into the vibe-proxy binary and is never downloaded at
runtime.
