# -*- coding: utf-8 -*-
"""金标准对比驱动脚本：用原 Python 版 run() 以相同参数跑同一份夹具。

用法（在 e:\\workgo\\work\\doc_batch_generator 下执行）：
    python docgen/cmd/pygolden/driver.py

输出写入 docgen/testdata/out_python，与 Go 版输出 docgen/testdata/out 对比。
"""
import json
import os
import shutil
import sys

BASE = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))  # docgen
ROOT = os.path.dirname(BASE)                                                        # 工程根
sys.path.insert(0, ROOT)

import doc_batch_generator  # noqa: E402

FIX = os.path.join(BASE, "testdata")
OUT = os.path.join(FIX, "out_python")

with open(os.path.join(BASE, "run.json"), encoding="utf-8") as f:
    cfg = json.load(f)


def abs_path(p):
    return p if os.path.isabs(p) else os.path.join(BASE, p)


# 准备输出目录（Python 版要求目录已存在）
if os.path.isdir(OUT):
    shutil.rmtree(OUT)
os.makedirs(OUT)

params = {
    "data_source": "local",
    "local_file": abs_path(cfg["excel_path"]),
    "header_row": cfg["header_row"],
    "output_folder": OUT,
    "create_subfolder": "yes" if cfg.get("create_subfolder") else "no",
    "batch_config": {
        "sheet_name": cfg["sheet_name"],
        "content_rules": [dict(r, template_folder=abs_path(r["template_folder"]))
                          for r in cfg.get("content_rules", []) if r.get("enabled", True)],
        "number_rules": [dict(r, mode="number", template_folder=abs_path(r["template_folder"]))
                         for r in cfg.get("number_rules", []) if r.get("enabled", True)],
        "naming": {"items": cfg["naming_items"]},
        "groups": cfg.get("groups", {}),
    },
}

result = doc_batch_generator.run(params)

print("success:", result["success"])
print("message:", result["message"])
print("---- logs ----")
for line in result["logs"]:
    print(line)
print("---- details ----")
for row in result["data"]["details"]:
    print(" | ".join(row))

if not result["success"]:
    sys.exit(1)
