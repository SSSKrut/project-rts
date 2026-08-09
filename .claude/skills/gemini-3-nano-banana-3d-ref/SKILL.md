---
name: gemini-3-nano-banana-3d-ref
description: "Use when: writing a Gemini-3 Nano Banana image prompt for a 3D reference of any object between low-poly and mid-poly, with separated parts, slight angle view, and no text."
argument-hint: "Object/subject, parts list, angle, background, lighting"
---

# Gemini-3 Nano Banana — 3D Reference Prompt (Separated Parts)

## When to Use
- You need a single English prompt for Gemini-3 Nano Banana to generate a 3D reference image of any object.
- You want a look between low-poly and mid-poly with simplified geometry.
- You need the main parts separated and shown at a slight angle (not profile), with no text.

## Inputs
- **Required:** subject/object (e.g., “M1 Abrams tank”, “apple”, “human character”, “spaceship”).
- **Optional:** list of major parts to separate, angle, background color, lighting style, surface detail level.

## Procedure
1. If the subject is missing, ask the user to specify it.
2. Derive or confirm the major parts to separate (match the subject).
3. Set the style: between low-poly and mid-poly, clean hard-surface or clean organic forms.
4. Specify layout: parts laid out side-by-side on a single plane with small gaps (not floating), all oriented consistently.
5. Specify view: slight 3/4 angle with a mild top-down view so forms read clearly.
6. Ensure composition: all parts fully visible, evenly spaced, not cropped.
7. Add rendering constraints: neutral background, soft even lighting, minimal textures.
8. Add exclusions: no labels, no text, no watermark, no extra props.
9. Output one ready-to-use English prompt only (no commentary, no markdown).

## Safety/Clarity Note
- For living subjects (people/animals), request mannequin-style segmented parts with clean, non-gory separation.

## Default Prompt Template (fill in subject + parts)
3D reference render of [SUBJECT], between low-poly and mid-poly, clean shapes with simplified surface detail. Parts laid out side-by-side on a single plane with small gaps (not floating): [PARTS]. All parts shown at a consistent 3/4 angle with a slight top-down view, aligned and evenly spaced, nothing cropped. Neutral gray studio background, soft even lighting, subtle shadows. No text, no labels, no watermark, no extra objects.
