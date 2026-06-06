# NPU 実行環境の導入手順 — Miniforge(conda) + PATH 登録 + AMD Ryzen AI SDK (20260607 01:36)

## Issue

`start.ps1 -Backend npu`（および既定 auto）は NPU シム `npu/npu_server.py` を
`conda run -n ryzen-ai-1.7.1 python ...` で起動する。そのために必要な **Miniforge(conda) の導入と
システム PATH 登録、AMD Ryzen AI SDK の導入**の手順を記録する。Vulkan/CPU 経路はこれらは不要
（`winget install ggml.llamacpp` のみ）。

## Learnings（導入手順 — Windows 11, 管理者 PowerShell）

### 1. AMD NPU ドライバ（XDNA2）
- AMD 指定の WHQL NPU ドライバを `npu_sw_installer.exe`（管理者）で導入。`デバイス マネージャー` に
  NPU（AI Boost / XDNA）が出ること。Ryzen AI の quicktest がドライバ不一致を出したら AMD 指定版へ更新。

### 2. Miniforge (conda) の導入と PATH 登録 ★今回の要点
- 導入（winget）:
  ```powershell
  winget install --id CondaForge.Miniforge3 -e
  ```
  既定の導入先は `C:\Users\<you>\miniforge3` または `C:\ProgramData\miniforge3`（マシンスコープ導入時）。
- **`conda` を PATH から呼べるようにする**（start.ps1 は `conda run` を使うため必須）。いずれか:
  - (推奨) `conda init powershell` を一度実行し、新しい PowerShell を開く（プロファイルで conda が有効化される）。
  - もしくは PATH に **`<miniforge>\Scripts`** と **`<miniforge>\condabin`** を追加:
    ```powershell
    # ユーザ環境変数 PATH に condabin を恒久追加（例）
    $cb = "C:\Users\$env:USERNAME\miniforge3\condabin"
    [Environment]::SetEnvironmentVariable("Path", ([Environment]::GetEnvironmentVariable("Path","User") + ";" + $cb), "User")
    ```
    新しいターミナルで `conda --version` が通れば OK。
  - PATH に通したくない場合は `start.ps1 -Conda "C:\Users\<you>\miniforge3\condabin\conda.bat"` で絶対パス指定も可。
- 確認:
  ```powershell
  conda --version
  conda env list      # 次の手順後に ryzen-ai-1.7.1 が現れる
  ```

### 3. AMD Ryzen AI Software 1.7.1（SDK）
- **winget 非対応**。AMD アカウントポータルから `ryzen-ai-lt-1.7.1.exe` を入手し実行
  （`ryzen-ai-lt-*` は LLM 向けの軽量インストーラ）。前提に Miniforge(conda) が要る。
- インストーラが conda 環境 **`ryzen-ai-1.7.1`** を作成し、Ryzen AI EP
  （`C:\Program Files\RyzenAI\1.7.1\deployment\onnxruntime_providers_ryzenai.dll`）と numpy/onnxruntime/
  transformers を導入する。
- 確認:
  ```powershell
  conda activate ryzen-ai-1.7.1
  # AMD 同梱の quicktest があれば実行して NPU/ドライバ整合を確認
  ```

### 4. LFM2 NPU モデル（git-lfs）
- `git lfs` を有効化し、AMD プリビルドを取得（~2.5GB）:
  ```powershell
  git lfs install
  git clone https://huggingface.co/amd/LFM2-1.2B-ONNX_rai_1.7.1 C:\Users\<you>\ryzenai-lfm2\LFM2-1.2B-ONNX_rai_1.7.1
  ```
- 中身: `lfm2-1.2B-token-fusion.onnx` + `.onnx.data` + `.fconst` + `dd_metastate_*.ctrlpkt` +
  `ryzenai_ep_utils.py`（モデルディレクトリ同梱。シムが import 時に使用）。

### 5. 起動確認
```powershell
conda run -n ryzen-ai-1.7.1 python .\npu\npu_server.py `
    --model C:\Users\<you>\ryzenai-lfm2\LFM2-1.2B-ONNX_rai_1.7.1 --host 127.0.0.1 --port 8792
# 別シェル: Invoke-WebRequest http://127.0.0.1:8792/health  -> 200
.\start.ps1               # auto が NPU を選択（ログ: Selected backend: npu）
```
初回は NPU オーバレイのコンパイルで数分かかる（socket はモデルロード後に開く）。`start.ps1` は
`-HealthTimeoutSec`（既定 600s）まで `/health` を待つ。

### 注意 / 落とし穴
- **PATH 未登録**だと `start.ps1` の `conda run` が失敗し、auto は NPU をスキップして Vulkan に降格する
  （Test-NpuAvailable が conda env を検出できないため）。`-Backend npu` で強制すると Hint を出して失敗する。
- env 名がインストーラのバージョンで変わる場合は `start.ps1 -CondaEnv "<name>"` で上書き。
- これらの前提は **NPU 経路専用**。Vulkan/CPU は不要（`winget install ggml.llamacpp` のみ）。

## Refs
- https://github.com/conda-forge/miniforge
- https://ryzenai.docs.amd.com/en/latest/inst.html
- https://huggingface.co/amd/LFM2-1.2B-ONNX_rai_1.7.1
- npu/README.md
- docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md
