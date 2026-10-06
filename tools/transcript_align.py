#!/usr/bin/env python3
"""Local audio/text alignment protocol. JSON stdin and JSON stdout only.

ASR wording is never corrected or generated here. Returned positions identify
verbatim source text; timings come from an acoustic forced-alignment model.
"""
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import unicodedata
import wave


def lexical(char):
    return char == "'" or unicodedata.category(char)[0] in "LN"


def source_words(text, items, offset_ms, duration_ms):
    positions = [i for i, c in enumerate(text) if lexical(c)]
    normalized = "".join(text[i] for i in positions)
    cursor = 0
    words = []
    for item in items:
        token = "".join(c for c in item.text if lexical(c))
        if not token:
            continue
        if normalized[cursor:cursor + len(token)] != token:
            raise ValueError("aligner tokens differ from ASR source")
        start, end = positions[cursor], positions[cursor + len(token) - 1] + 1
        cursor += len(token)
        t0, t1 = round(item.start_time * 1000), round(item.end_time * 1000)
        if t0 < 0 or t1 < t0 or t1 > duration_ms:
            raise ValueError("aligner time outside audio window")
        words.append({"text": text[start:end], "text_start": start, "text_end": end,
                      "start_ms": t0 + offset_ms, "end_ms": t1 + offset_ms,
                      "method": "forced_alignment"})
    if cursor != len(normalized):
        raise ValueError("aligner omitted source words")
    # A quantized time predictor can assign the same observed instant to fast
    # adjacent characters. Keep their full contiguous source range together
    # with an adjacent observed acoustic span, rather than fabricating times.
    merged = []
    pending = None
    for word in words:
        if pending and merged and any(c in '。！？!?；;.' for c in text[pending["text_end"]:word["text_start"]]):
            # Keep a quantized final character with its own sentence. Joining
            # it forward would fuse two sentence playback entries.
            merged[-1]["text_end"] = pending["text_end"]
            merged[-1]["text"] = text[merged[-1]["text_start"]:pending["text_end"]]
            merged[-1]["end_ms"] = max(merged[-1]["end_ms"], pending["end_ms"])
            pending = None
        if pending:
            word = {**word, "text_start": pending["text_start"],
                    "start_ms": min(pending["start_ms"], word["start_ms"])}
            word["text"] = text[word["text_start"]:word["text_end"]]
            pending = None
        if word["end_ms"] <= word["start_ms"]:
            pending = word
            continue
        merged.append(word)
    if pending:
        if not merged:
            raise ValueError("aligner returned no acoustic interval")
        merged[-1]["text_end"] = pending["text_end"]
        merged[-1]["text"] = text[merged[-1]["text_start"]:pending["text_end"]]
        merged[-1]["end_ms"] = max(merged[-1]["end_ms"], pending["end_ms"])
    return merged


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend", choices=["mlx", "qwen"], default="mlx" if sys.platform == "darwin" and platform.machine() == "arm64" else "qwen")
    parser.add_argument("--model")
    parser.add_argument("--language", default="Chinese")
    parser.add_argument("--cache-dir", default=str(Path.home() / ".cache" / "vidlens-alignment"))
    args = parser.parse_args()
    request = json.load(sys.stdin)
    cache = Path(args.cache_dir)
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    model_name = args.model or ("mlx-community/Qwen3-ForcedAligner-0.6B-8bit" if args.backend == "mlx" else "Qwen/Qwen3-ForcedAligner-0.6B")
    # Library progress/logs belong to stderr; stdout is reserved for the wire
    # protocol, including model loading and download progress.
    with contextlib.redirect_stdout(sys.stderr):
        import numpy as np
        model = None
        with tempfile.TemporaryDirectory(prefix="vidlens-align-") as tmp:
            wav = Path(tmp) / "audio.wav"
            subprocess.run([request.get("ffmpeg") or "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", request["audio_path"],
                            "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", str(wav)], check=True, stdout=subprocess.DEVNULL)
            with wave.open(str(wav), "rb") as f:
                audio_bytes = f.readframes(f.getnframes())
                audio = np.frombuffer(audio_bytes, dtype=np.int16).astype(np.float32) / 32768
            audio_hash = hashlib.sha256(audio_bytes).hexdigest()
            response = []
            for row in request["rows"]:
                text = row["content"].strip()
                start, end = row["window_start_ms"], row["window_end_ms"]
                if row["status"] != "completed" or end <= start or start < 0:
                    raise ValueError("incomplete or untimed ASR window")
                key = hashlib.sha256(json.dumps(["source-offset-v2", args.backend, model_name, args.language, audio_hash, start, end, text], ensure_ascii=False).encode()).hexdigest()
                cached = cache / (key + ".json")
                if cached.exists():
                    words = json.loads(cached.read_text())
                elif not any(lexical(c) for c in text):
                    words = []
                else:
                    if model is None:
                        if args.backend == "mlx":
                            from mlx_audio.stt import load
                            model = load(model_name)
                        else:
                            import torch
                            from qwen_asr import Qwen3ForcedAligner
                            model = Qwen3ForcedAligner.from_pretrained(model_name, dtype=torch.float32, device_map="cpu")
                    clip = audio[round(start * 16):round(end * 16)]
                    result = model.generate(audio=clip, text=text, language=args.language) if args.backend == "mlx" else model.align(audio=(clip, 16000), text=text, language=args.language)[0]
                    words = source_words(text, result.items, start, end - start)
                    fd, temp = tempfile.mkstemp(dir=cache)
                    try:
                        with os.fdopen(fd, "w") as f:
                            json.dump(words, f, ensure_ascii=False)
                        os.replace(temp, cached)
                    finally:
                        if os.path.exists(temp):
                            os.unlink(temp)
                response.append({"chunk_index": row["chunk_index"], "words": words})
                print(f"aligned window {len(response)}/{len(request['rows'])}", file=sys.stderr, flush=True)
    json.dump({"rows": response}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    try:
        main()
    except ImportError:
        # A whitelisted machine-readable error lets the API explain a broken
        # virtual environment without exposing tracebacks or private paths.
        json.dump({"error_code": "dependencies_missing"}, sys.stdout)
        sys.exit(1)
