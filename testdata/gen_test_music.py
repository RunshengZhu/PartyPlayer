#!/usr/bin/env python3
"""生成结构与真实曲库一致的合成测试音乐（无版权问题）。

结构对齐 resources/舞会曲库：根目录/舞种/音频文件，并包含
special/圣诞特色曲目/ 这类舞种内嵌套子目录用例。
每个文件是 8 秒单声道正弦波 WAV，频率按曲目编号变化。
"""
import math
import os
import struct
import sys
import wave

ROOT = os.path.join(os.path.dirname(__file__), "music")
SR = 22050
DURATION = 8

# 舞种 -> 曲目数；special 额外带嵌套子目录
PLAN = {
    "慢三": 2,
    "华尔兹": 3,
    "恰恰": 2,
    "伦巴": 2,
    "探戈": 2,
    "桑巴": 2,
    "牛仔": 2,
    "平四": 2,
    "special": 1,
}


def write_wav(path: str, freq: float) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    w = wave.open(path, "w")
    w.setnchannels(1)
    w.setsampwidth(2)
    w.setframerate(SR)
    frames = bytearray()
    for t in range(SR * DURATION):
        v = int(11000 * math.sin(2 * math.pi * freq * t / SR) * (0.7 + 0.3 * math.sin(t / 6000)))
        frames += struct.pack("<h", v)
    w.writeframes(bytes(frames))
    w.close()


def main() -> None:
    n = 1
    for genre, count in PLAN.items():
        for i in range(1, count + 1):
            freq = 220 * (2 ** ((i % 8) / 6.0))
            write_wav(os.path.join(ROOT, genre, f"{genre}测试{i:02d}.wav"), freq)
            n += 1
    # 嵌套子目录：special/圣诞特色曲目/
    for i in range(1, 3):
        write_wav(os.path.join(ROOT, "special", "圣诞特色曲目", f"圣诞特色{i:02d}.wav"), 330 + i * 40)
        n += 1
    print(f"已生成 {n - 1} 个测试音频于 {ROOT}")


if __name__ == "__main__":
    sys.exit(main())
