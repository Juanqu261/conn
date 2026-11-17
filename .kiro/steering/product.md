---

## inclusion: always

# Conn product direction

Conn turns a software objective into a versioned team blueprint, divides accountability into human-agent work cells, and helps the team verify that independently produced parts still fit together.

## Product boundaries

- Conn is a coordination protocol and evidence layer, not an agent launcher or supervising agent.
- Humans approve objectives, accept cell charters, and remain accountable for agent-assisted work.
- Agents may propose clarifications, partitions, contracts, and evidence. They may not approve their own proposals or silently change accepted boundaries.
- Cell charters are advisory work boundaries, not file locks. Boundary changes must be proposed and recorded.
- Collision detection supports blueprint reconciliation; it is not the product's primary organizing feature.
- Conn should complement the team's IDE, issue tracker, Git host, CI, and review tools rather than replace them.
- Conn must not implement employee productivity scoring.

## MVP boundary

- Test one objective delivered by three humans operating three independent Kiro sessions as work cells.
- Implement and validate the Kiro client integration only.
- Keep protocol concepts transport-neutral so other coding-agent integrations can be researched after the MVP.
- Retain GitHub as the initial repository host unless a later product decision changes it.
