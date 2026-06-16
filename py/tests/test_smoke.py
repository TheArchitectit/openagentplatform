"""Smoke tests — verify the oap package is importable and reports a version."""


def test_import() -> None:
    import oap

    assert oap is not None


def test_version() -> None:
    import oap

    assert isinstance(oap.__version__, str)
    assert len(oap.__version__.split(".")) == 3


def test_settings_loads() -> None:
    from oap.settings import get_settings

    s = get_settings()
    assert s.app_env in {"development", "staging", "production", "test", ""}
    assert s.jwt_audience
