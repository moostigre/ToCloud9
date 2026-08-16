# Client assets

`Interface/AddOns/SWPQol` owns the Initiate Riding notification shown when the
module grants spell 80860. `merge-into-swp.txt` lets the launcher compose that
Lua file into its managed SWP addon without keeping a second copy.

The launcher's DBC builder also derives the Sprint tooltip and amount from the
installed module configuration and writes the required records into the shared
`patch-T.MPQ`.
