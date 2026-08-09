# Client assets

The launcher should copy `Interface/AddOns/SWPMultispecs` into the matching
client directory. The addon supplies the third-spec controls missing from the
stock 3.3.5a interface and reflects the server-configured unlock levels.

`dbc/` and `patch/` are reserved for future DBC or MPQ-ready assets. This
version requires no DBC records or executable binary patch.
SWPMultispecs 1.2.0 adds the talent-window purchase controls used by the gated
dual- and triple-specialization system. The server remains authoritative for
level, price, character purchase, and character entitlement checks; the addon
only displays state and sends the corresponding player commands.
