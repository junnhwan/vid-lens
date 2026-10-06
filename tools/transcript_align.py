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
import time
import importlib.metadata
from alignment_model import ModelIdentityError, prepare, validate, atomic_json


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


PREPROCESSING = 'ffmpeg-pcm-s16le-mono-16000-v1'
OUTPUT_SCHEMA = 'source-offset-v2'


def runtime_versions(backend):
    names = ['numpy', 'transformers', 'mlx-audio', 'mlx'] if backend == 'mlx' else ['numpy', 'transformers', 'qwen-asr', 'torch']
    return {name: importlib.metadata.version(name) for name in names}


def check_runtime(backend):
    # Imports load libraries, never a model or remote hub resources.
    import numpy
    if backend == 'mlx':
        import mlx.core
        from mlx_audio.stt import load
    else:
        import torch
        from qwen_asr import Qwen3ForcedAligner
    versions = runtime_versions(backend)
    if versions['mlx-audio' if backend == 'mlx' else 'qwen-asr'] != ('0.5.7' if backend == 'mlx' else '0.0.6'):
        raise ModelIdentityError('runtime_version_mismatch')
    return versions


def cache_identity(manifest, backend, language, versions, audio_hash, start, end, text):
    return hashlib.sha256(json.dumps([OUTPUT_SCHEMA, PREPROCESSING, manifest['identity'],
        manifest['revision'], backend, language, versions,
        {'device': 'mlx' if backend == 'mlx' else 'cpu', 'dtype': 'model' if backend == 'mlx' else 'float32'},
        audio_hash, start, end, text], ensure_ascii=False, sort_keys=True).encode()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend", choices=["mlx", "qwen"], default="mlx" if sys.platform == "darwin" and platform.machine() == "arm64" else "qwen")
    parser.add_argument("--model")
    parser.add_argument("--language", default="Chinese")
    parser.add_argument("--cache-dir", default=str(Path.home() / ".cache" / "vidlens-alignment"))
    parser.add_argument('--revision')
    parser.add_argument('--prepare-model-manifest', action='store_true')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    if args.prepare_model_manifest:
        manifest = prepare(args.model or '', args.revision, args.backend)
        json.dump({'model_identity': manifest['identity'], 'revision': manifest['revision']}, sys.stdout)
        return
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
    # A local fixed installation is mandatory: no remote-name fallback/download.
    manifest = validate(args.model, args.backend, args.revision)
    if args.check:
        with contextlib.redirect_stdout(sys.stderr):
            versions = check_runtime(args.backend)
        json.dump({'available': True, 'health': 'selfcheck_ok', 'backend': args.backend,
                   'revision': manifest['revision'], 'model_identity': manifest['identity'], 'versions': versions}, sys.stdout)
        return
    versions = runtime_versions(args.backend)

    started = time.monotonic()
    cache_hits, model_loads = 0, 0
    request = json.load(sys.stdin)
    cache = Path(args.cache_dir)
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    model_name = str(Path(args.model).resolve())
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
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
                key = cache_identity(manifest, args.backend, args.language, versions, audio_hash, start, end, text)
                cached = cache / (key + ".json")
                if cached.exists():
                    cache_hits += 1
                    words = json.loads(cached.read_text(encoding="utf-8"))
                elif not any(lexical(c) for c in text):
                    words = []
                else:
                    if model is None:
                        model_loads += 1
                        if args.backend == "mlx":
                            from mlx_audio.stt import load
                            model = load(model_name)
                        else:
                            import torch
                            from qwen_asr import Qwen3ForcedAligner
                            model = Qwen3ForcedAligner.from_pretrained(model_name, dtype=torch.float32, device_map="cpu", local_files_only=True)
                    clip = audio[round(start * 16):round(end * 16)]
                    result = model.generate(audio=clip, text=text, language=args.language) if args.backend == "mlx" else model.align(audio=(clip, 16000), text=text, language=args.language)[0]
                    words = source_words(text, result.items, start, end - start)
                    validate(args.model, args.backend, args.revision)
                    atomic_json(cached, words)
                response.append({"chunk_index": row["chunk_index"], "words": words})
                print(f"aligned window {len(response)}/{len(request['rows'])}", file=sys.stderr, flush=True)
    validate(args.model, args.backend, args.revision)
    json.dump({"rows": response, "runtime": {"cache_hits": cache_hits, "model_loads": model_loads, "elapsed_ms": round((time.monotonic() - started) * 1000), "model_identity": manifest["identity"]}}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    try:
        main()
    except (ModelIdentityError, FileNotFoundError, PermissionError) as error:
        code = str(error) if isinstance(error, ModelIdentityError) else 'model_missing'
        json.dump({'error_code': code}, sys.stdout)
        sys.exit(1)
    except (ImportError, importlib.metadata.PackageNotFoundError):
        # A whitelisted machine-readable error lets the API explain a broken
        # virtual environment without exposing tracebacks or private paths.
        json.dump({"error_code": "dependencies_missing"}, sys.stdout)
        sys.exit(1)
