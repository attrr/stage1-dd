from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from type_hints import QemuMachine
    import tests.failover.lib as lib

    machine: QemuMachine
    nodes: Any = Any
