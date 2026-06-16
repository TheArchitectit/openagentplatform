"""Application settings loaded from environment variables."""

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    app_env: str = Field(default="development")
    log_level: str = Field(default="info")

    postgres_dsn: str = Field(default="postgresql+asyncpg://oap:oap@localhost:5432/oap")
    nats_url: str = Field(default="nats://localhost:4222")
    nats_cert_file: str | None = None
    nats_key_file: str | None = None
    nats_ca_file: str | None = None

    oidc_issuer_url: str = Field(default="http://localhost:5556/dex")
    oidc_client_id: str = Field(default="oap-web")
    oidc_client_secret: str = Field(default="oap-web-secret")
    jwt_secret: str = Field(default="dev-secret-change-me")
    jwt_audience: str = Field(default="oap")
    jwt_algorithm: str = Field(default="HS256")

    sentry_dsn: str | None = None


def get_settings() -> Settings:
    return Settings()
