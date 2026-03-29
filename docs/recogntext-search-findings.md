# RECOGNTEXT Device Search — Definitive Findings

Date: 2026-03-26
Device: Supernote Nomad A6X2 (SN078C10034074)
Firmware: Chauvet.E003.2512251001.2157_release

## Summary

API-injected RECOGNTEXT in Standard notes IS searchable via the Supernote's built-in handwriting search. The only requirements are:

1. **`RECOGNSTATUS:1`** set in per-page metadata (go-sn's `InjectRecognText` does this)
2. **Valid JIIX JSON** in the RECOGNTEXT block (go-sn's `BuildRecognText` does this)

No changes to `FILE_RECOGN_TYPE` or `RECOGNTYPE` are necessary. The file stays Standard.

## Confirmed Working State

```
Header:
  FILE_RECOGN_TYPE: 0     ← stays 0 (Standard)
  FILE_RECOGN_LANGUAGE: none

Per-page metadata:
  RECOGNSTATUS: 1         ← set by go-sn InjectRecognText
  RECOGNTYPE: 0           ← unchanged
  RECOGNTEXT: <offset>    ← points to injected JIIX block
  RECOGNFILE: 0           ← no MyScript iink sidecar
  RECOGNFILESTATUS: 0     ← no sidecar
```

## JIIX Format (verified working)

```json
{
  "type": "Raw Content",
  "elements": [
    {
      "type": "Text",
      "label": "Full text of element",
      "words": [
        {"label": "Full", "bounding-box": {"x": 10.07, "y": 11.08, "width": 15.44, "height": 9.44}},
        {"label": " "},
        {"label": "text", "bounding-box": {"x": 26.55, "y": 10.75, "width": 9.18, "height": 9.52}}
      ]
    }
  ]
}
```

Key JIIX requirements:
- Root `type` must be `"Raw Content"`
- Elements array with `type: "Text"` entries
- `label` field on Text elements = concatenation of all word labels
- Words with `bounding-box` (x, y, width, height in millimeters) for real words
- Space separators: `{"label": " "}` (no bounding-box)
- Newline separators: `{"label": "\n"}` (no bounding-box)
- No `version` field needed (device omits it, search doesn't check it)

## What Does NOT Work

- `RECOGNTYPE:1` alone (per-page) — does not enable search
- `FILE_RECOGN_TYPE:1` alone (header) — triggers RTR mode, demands recognition language
- `FILE_RECOGN_TYPE:1` + `FILE_RECOGN_LANGUAGE:en_US` — makes search work but note displays as RTR and may trigger AUTO_CONVERT

## Investigation History

### The Misleading Decompilation

Decompiling `SupernoteSearch.apk` with jadx showed this gate in `CheckUtils.isRecognitionNote()`:

```java
if (SuperNoteUtils.superNoteNote.fetchFileRecognType(filePath) != 1) return false;
```

This suggested `FILE_RECOGN_TYPE:1` was required. However, empirical testing proved this is NOT the actual behavior — Standard notes with `FILE_RECOGN_TYPE:0` pass this check and are searchable. The decompiled control flow is likely inaccurate (common with jadx on complex bytecode).

### The Native Function

`fetchFileRecognType(String filePath)` is a JNI function in `libratta_sn_process.so`. Despite the name suggesting it reads `FILE_RECOGN_TYPE`, it appears to check `RECOGNSTATUS` (whether recognition data exists on any page) rather than the file-level recognition type flag.

### What Actually Gates Search

The search app (`com.ratta.search`) flow:
1. `getFileInfo` — finds `.note` files on device (returns `infoBeanList`)
2. `processFileInfo` — filters by search mode (HANDWRITING → `recognitionSearch`)
3. `isRecognitionNote` — calls native `fetchFileRecognType` — **returns 1 when RECOGNSTATUS:1 is set on pages, regardless of FILE_RECOGN_TYPE**
4. `RecognitionSearchTask.execute()` — for each page, checks `fetchPageRecognStatus == 1`, then reads RECOGNTEXT block, parses JIIX, searches `element.label`

### Red Herrings Along the Way

1. **`RECOGNTYPE` per-page flag** — Setting this to 1 appeared to help in one test but was actually a caching artifact. Not required.
2. **`FILE_RECOGN_TYPE:1` header flag** — Makes search work but makes the note display as RTR. Not required and causes side effects.
3. **Earlier test failures** — Were caused by file sync issues (wrong `path_display` in `list_folder`, files disappearing from device, timing issues with OCR injection vs download), not by missing recognition flags.

## RTR Notes — Why They're Different

| | Standard | RTR |
|---|---|---|
| `FILE_RECOGN_TYPE` | `0` | `1` |
| `FILE_RECOGN_LANGUAGE` | `none` | `en_US` (or other) |
| `RECOGNFILE` | `0` | Non-zero offset to MyScript iink data |
| `RECOGNFILESTATUS` | `0` | `1` |
| AUTO_CONVERT | Does not fire | Fires ~40s after opening, clobbers RECOGNTEXT |
| API injection safe? | Yes | No — device overwrites injected text |

## Reproduction Steps

1. Create a Standard note on the Supernote device with handwriting
2. Sync to server (SPC or NoteBridge)
3. Run OCR on the rendered page image (any vision LLM)
4. Inject RECOGNTEXT using `go-sn`'s `InjectRecognText` + `BuildRecognText`
5. Update the file catalog MD5 so the device re-downloads the modified file
6. Sync again — device downloads the injected version
7. Search on device finds the OCR text

Verified surviving:
- Device reboot
- Search app force-stop and restart
- Multiple sync cycles
- Opening and closing the note on the device
- Multiple pages (3-page note tested)

## go-sn Library Version

Verified with: `github.com/jdkruzr/go-sn` @ `b2a5f8c9e7e4` (2026-03-22)

The `c44531c` commit (adding `RECOGNTYPE:1`) is harmless but not required for search functionality.
