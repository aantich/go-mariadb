"""Explicit checks for suites sharing an owned-server lifecycle controller."""
from dataclasses import dataclass
from typing import Callable


@dataclass(frozen=True)
class ProfileSuite:
    contract: str
    method: str
    required: dict
    commands: Callable
    assess: Callable
    validate: Callable
    history: Callable
    passing: Callable
