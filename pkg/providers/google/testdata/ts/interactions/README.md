# TS interactions fixtures

These files are copied from the TypeScript AI SDK at commit `b658e8503c`
(`packages/google/src/interactions/__fixtures__/`) and drive
`ts_interactions_fixtures_test.go`.

All files are byte-identical to TS except the four image fixtures
(`image-output.json`, `image-output-modify.json`, `image-output.chunks.txt`,
`image-output-modify.chunks.txt`). In those, two string values were shrunk to
keep the repository small (the originals are 5 to 6 MB each):

- `data` (the base64 image) is a 1x1 baseline JPEG. The originals are JPEG,
  so media-type detection is unchanged.
- `signature` (the opaque base64 thought signature) is cut to its first 64
  characters.

All other fields, the JSON layout, and the chunk boundaries of the
`*.chunks.txt` files are unchanged.

## Regenerating

Copy the four files from the TS repository again, then shrink them with the
same rule: replace each `"data"` or `"signature"` string value longer than
1000 characters as described above, leaving every other byte as is. A
regex substitution over the raw file text is enough, for example in Python:

```python
import re
TINY_JPEG = "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP" + "/" * 86 + "wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA="
def shrink(m):
    key, value = m.group(2), m.group(3)
    if len(value) <= 1000:
        return m.group(0)
    return m.group(1) + (TINY_JPEG if key == "data" else value[:64]) + m.group(4)
pattern = re.compile(r'("(data|signature)"\s*:\s*")((?:[^"\\]|\\.)*)(")')
# text = pattern.sub(shrink, text)
```
