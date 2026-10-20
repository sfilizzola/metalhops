# Discovery text strips spaces; Normalized name keeps them

Matching a Venue during Discovery and deciding that two names are the same
Venue are different questions. Discovery text keeps only letters and digits,
so `Saint-Étienne` and `Saint Etienne` are the same city and `IronMaiden`
finds `Iron Maiden`. Normalized name collapses spaces to one and keeps them,
so those two names stay distinct for ordering and for a Duplicate Venue
Submission. One spelling for both would either treat `Iron Maiden` and
`IronMaiden` as duplicates or make `Saint Etienne` miss `Saint-Étienne`.

Case and accent folding alone was rejected. It would keep the hyphen and the
space, so those two city spellings would not match.

The discovery spelling must not replace `venue_name_key`. That key is the
Normalized name.
