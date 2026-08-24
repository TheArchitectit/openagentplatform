"""
Per-organisation budget enforcement for the OAP adapter subsystem.

Tracks cumulative spend per organisation and fires threshold-based alerts
(80 %, 90 %, 100 %) as spend approaches the configured monthly limit.
"""

from __future__ import annotations

from oap.adapters.cost_models import (
    AlertSeverity,
    BudgetAlert,
    BudgetLimit,
    _THRESHOLDS,
)


class BudgetTracker:
    """Tracks cumulative spend per organisation and fires threshold alerts.

    Maintains a per-org running total for the current calendar month. When
    cumulative spend crosses 80 %, 90 %, or 100 % of the configured limit, a
    BudgetAlert is generated and recorded. Each threshold fires at most
    once per billing period.

    Attributes:
        _limits: Per-organisation budget limits keyed by org_id.
        _spend: Cumulative spend per org for the current period.
        _alerts: Alerts generated in the current period.
        _fired: Tracks which thresholds have already fired per org.
    """

    def __init__(self) -> None:
        self._limits: dict[str, BudgetLimit] = {}
        self._spend: dict[str, float] = {}
        self._alerts: list[BudgetAlert] = []
        self._fired: dict[str, set[int]] = {}

    def set_limit(self, org_id: str, monthly_limit: float, currency: str = "USD") -> None:
        """Set or update the monthly budget limit for an organisation.

        Args:
            org_id: Organisation identifier.
            monthly_limit: Maximum monthly spend in the given currency.
            currency: ISO 4217 currency code.
        """
        self._limits[org_id] = BudgetLimit(
            org_id=org_id,
            monthly_limit=monthly_limit,
            currency=currency,
        )
        if org_id not in self._spend:
            self._spend[org_id] = 0.0
            self._fired[org_id] = set()

    def add_spend(self, org_id: str, amount: float) -> list[BudgetAlert]:
        """Record additional spend and check for threshold crossings.

        Args:
            org_id: Organisation identifier.
            amount: Cost increment in USD.

        Returns:
            A list of newly triggered BudgetAlerts (may be empty).
        """
        if org_id not in self._limits:
            return []

        self._spend[org_id] = self._spend.get(org_id, 0.0) + amount
        limit = self._limits[org_id].monthly_limit
        if limit <= 0:
            return []

        pct = (self._spend[org_id] / limit) * 100.0
        fired = self._fired.setdefault(org_id, set())
        new_alerts: list[BudgetAlert] = []

        for threshold in _THRESHOLDS:
            if pct >= threshold and threshold not in fired:
                fired.add(threshold)
                severity = (
                    AlertSeverity.CRITICAL
                    if threshold == 100
                    else AlertSeverity.WARNING
                    if threshold == 90
                    else AlertSeverity.INFO
                )
                alert = BudgetAlert(
                    org_id=org_id,
                    threshold_pct=threshold,
                    severity=severity,
                    spent=round(self._spend[org_id], 6),
                    limit=limit,
                    message=(
                        f"Organisation {org_id} has spent "
                        f"${self._spend[org_id]:.2f} of ${limit:.2f} "
                        f"({pct:.1f}%) — {threshold}% threshold reached."
                    ),
                )
                self._alerts.append(alert)
                new_alerts.append(alert)

        return new_alerts

    def get_spend(self, org_id: str) -> float:
        """Return the current cumulative spend for an organisation."""
        return self._spend.get(org_id, 0.0)

    def get_limit(self, org_id: str) -> float:
        """Return the configured monthly limit, or 0.0 if none set."""
        entry = self._limits.get(org_id)
        return entry.monthly_limit if entry else 0.0

    def get_alerts(self, org_id: str | None = None) -> list[BudgetAlert]:
        """Return alerts, optionally filtered by organisation.

        Args:
            org_id: If provided, return only alerts for this org.

        Returns:
            A list of BudgetAlert objects.
        """
        if org_id is None:
            return list(self._alerts)
        return [a for a in self._alerts if a.org_id == org_id]

    def reset(self, org_id: str | None = None) -> None:
        """Reset spend, fired thresholds, and alerts.

        Args:
            org_id: If provided, reset only this org. If None, reset all.
        """
        if org_id is None:
            self._spend.clear()
            self._fired.clear()
            self._alerts.clear()
        else:
            self._spend[org_id] = 0.0
            self._fired.pop(org_id, None)
            self._alerts = [a for a in self._alerts if a.org_id != org_id]
