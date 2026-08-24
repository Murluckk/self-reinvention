"""HTTP-обёртка над faster-whisper.

ML живёт здесь, а не в Go-боте: питоновский стек распознавания зрелый и
обновляется отдельно от бота, а обмен по локальному HTTP стоит копейки.
Сервис слушает только localhost — наружу его выставлять незачем.
"""

import logging
import os
import tempfile
import time
from contextlib import asynccontextmanager

from fastapi import FastAPI, File, HTTPException, UploadFile
from faster_whisper import WhisperModel

MODEL_NAME = os.getenv("WHISPER_MODEL", "small")
DEVICE = os.getenv("WHISPER_DEVICE", "cpu")
COMPUTE_TYPE = os.getenv("WHISPER_COMPUTE", "int8")
LANGUAGE = os.getenv("WHISPER_LANG", "ru")
BEAM_SIZE = int(os.getenv("WHISPER_BEAM", "5"))
MAX_BYTES = int(os.getenv("WHISPER_MAX_BYTES", str(64 * 1024 * 1024)))

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("asr")

model: WhisperModel | None = None


@asynccontextmanager
async def lifespan(_: FastAPI):
    """Модель грузится один раз на старте: на CPU это десятки секунд."""
    global model
    started = time.monotonic()
    log.info("гружу модель %s (%s, %s)", MODEL_NAME, DEVICE, COMPUTE_TYPE)
    model = WhisperModel(MODEL_NAME, device=DEVICE, compute_type=COMPUTE_TYPE)
    log.info("модель готова за %.1f с", time.monotonic() - started)
    yield
    model = None


app = FastAPI(title="tracker-asr", lifespan=lifespan)


@app.get("/health")
def health() -> dict:
    return {"ok": model is not None, "model": MODEL_NAME}


@app.post("/transcribe")
async def transcribe(file: UploadFile = File(...)) -> dict:
    if model is None:
        raise HTTPException(status_code=503, detail="модель ещё грузится")

    data = await file.read()
    if not data:
        raise HTTPException(status_code=400, detail="пустой файл")
    if len(data) > MAX_BYTES:
        raise HTTPException(status_code=413, detail="файл слишком большой")

    with tempfile.NamedTemporaryFile(suffix=".wav", delete=True) as tmp:
        tmp.write(data)
        tmp.flush()
        started = time.monotonic()
        segments, info = model.transcribe(
            tmp.name,
            language=LANGUAGE,
            beam_size=BEAM_SIZE,
            vad_filter=True,
            condition_on_previous_text=False,
        )
        text = " ".join(segment.text.strip() for segment in segments).strip()
        elapsed = time.monotonic() - started

    log.info("распознал %.1f с аудио за %.1f с: %r", info.duration, elapsed, text[:120])
    return {"text": text, "language": info.language, "duration": info.duration}
