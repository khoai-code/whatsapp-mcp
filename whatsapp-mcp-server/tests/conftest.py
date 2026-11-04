import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import config


@pytest.fixture(autouse=True)
def ignore_real_settings(monkeypatch, tmp_path):
    """Never read the developer's real .env: it may have writing switched on."""
    monkeypatch.setattr(config, "CONFIG_FILE", tmp_path / "no-such.env")
