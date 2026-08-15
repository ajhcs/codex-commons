# Open Sans font provenance

These are the upstream Open Sans variable fonts distributed by the
[`google/fonts`](https://github.com/google/fonts) project under the SIL Open
Font License 1.1. `OFL.txt` is retained beside the binaries.

The repository pins the exact reviewed bytes by SHA-256:

- `OpenSans-Variable.ttf` — `36643644f318a812aab2d2ed3bb98f8cf0872527f835fe9398d95fe6b9adb878`
- `OpenSans-Variable.woff2` — `8dbf3d44655c72437f2a9acc46579058dc7d3f82b2231cfdd82d0d6d61145674`
  (deterministically compressed from the pinned normal variable TTF with fontTools 4.62.1; this is the browser-served copy)
- `OpenSans-Italic-Variable.ttf` — `fe269381e992f32e135801740998544d6235061e37c93ec067ad2be3edd5b17b`
- `OFL.txt` — `fbbbcfef55318de350562559b671360de6d597112ecc5c73881b05092db89602`

The font metadata identifies “The Open Sans Project Authors” and the upstream
project as the source. The application serves the checked-in WOFF2 copy
directly and makes no runtime font request. The upstream TTFs remain beside it
for provenance and review; the unused italic face is not included in browser
CSS or the production bundle.
