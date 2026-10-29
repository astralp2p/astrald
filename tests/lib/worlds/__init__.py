"""Worlds: the machines one planned world of a run stands on.

A run is a list of worlds (lib/plan.py), and each world is opened fresh,
runs its steps, and closes. A world owns machines only: the runner owns the
facts, the verdicts and the session view.
"""
