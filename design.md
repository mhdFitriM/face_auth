---
version: alpha
name: Mobbin Light
description: A clean, editorial product marketing system with strong contrast, generous whitespace, and one vivid accent.
colors:
  primary: "#141414"
  primary-strong: "#000000"
  secondary: "#707070"
  tertiary: "#E5E7EB"
  neutral: "#FFFFFF"
  surface: "#FFFFFF"
  on-surface: "#141414"
  muted: "#F5F5F5"
  border: "#E5E7EB"
  success: "#7EE24B"
  error: "#D92D20"
typography:
  headline-display:
    fontFamily: Saans
    fontSize: 56px
    fontWeight: 652
    lineHeight: 56px
    letterSpacing: -0.6px
  headline-lg:
    fontFamily: Saans
    fontSize: 43px
    fontWeight: 400
    lineHeight: 52px
    letterSpacing: 0px
  headline-md:
    fontFamily: Saans
    fontSize: 33px
    fontWeight: 400
    lineHeight: 40px
    letterSpacing: 0px
  headline-sm:
    fontFamily: Saans
    fontSize: 26px
    fontWeight: 400
    lineHeight: 31px
    letterSpacing: 0px
  body-lg:
    fontFamily: Saans
    fontSize: 20px
    fontWeight: 400
    lineHeight: 30px
    letterSpacing: 0px
  body-md:
    fontFamily: Saans
    fontSize: 16px
    fontWeight: 400
    lineHeight: 24px
    letterSpacing: 0px
  body-sm:
    fontFamily: Saans
    fontSize: 14px
    fontWeight: 400
    lineHeight: 20px
    letterSpacing: 0px
  label-lg:
    fontFamily: Saans
    fontSize: 16px
    fontWeight: 600
    lineHeight: 24px
    letterSpacing: 0px
  label-md:
    fontFamily: Saans
    fontSize: 16px
    fontWeight: 456
    lineHeight: 24px
    letterSpacing: 0px
  label-sm:
    fontFamily: Saans
    fontSize: 12px
    fontWeight: 600
    lineHeight: 16px
    letterSpacing: 0.02em
rounded:
  none: 0px
  sm: 4px
  md: 8px
  lg: 16px
  xl: 24px
  full: 9999px
spacing:
  xs: 8px
  sm: 24px
  md: 40px
  lg: 80px
  xl: 120px
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.neutral}"
    typography: "{typography.label-lg}"
    rounded: "{rounded.full}"
    padding: 0px 16px
    height: 44px
  button-secondary:
    backgroundColor: "{colors.neutral}"
    textColor: "{colors.on-surface}"
    typography: "{typography.label-lg}"
    rounded: "{rounded.full}"
    padding: 0px 16px
    height: 44px
  button-tertiary:
    backgroundColor: "transparent"
    textColor: "{colors.secondary}"
    typography: "{typography.label-md}"
    rounded: "{rounded.none}"
    padding: 0px
    height: 24px
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.md}"
    padding: 16px
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.full}"
    padding: 0px 16px
    height: 44px
---

# Mobbin Light

## Overview
Mobbin feels minimal, modern, and highly editorial, with a strong product-marketing tone rather than a dense application UI. The layout uses a lot of whitespace to keep attention on the headline, CTA pair, and brand proof, which gives the page a premium but approachable feel. The single bright green accent adds energy without breaking the otherwise restrained black-and-white system.

## Colors
- **Primary (#141414):** The main ink color for headings, navigation, and filled CTA buttons. It provides the high-contrast, crisp look that defines the brand.
- **Primary strong (#000000):** A deeper black reserved for the most emphatic iconography and strongest contrast moments.
- **Secondary (#707070):** A soft gray used for supporting copy, utility links, and less prominent navigation text.
- **Tertiary (#E5E7EB):** A light border-gray that helps define cards, inputs, and subtle dividers without adding visual weight.
- **Neutral (#FFFFFF):** The dominant background tone, keeping the interface airy, bright, and spacious.
- **Surface (#FFFFFF):** Card and control surfaces stay white so they blend naturally into the page with minimal chrome.
- **Muted (#F5F5F5):** A gentle off-white for quiet fills, hover states, and inset containers when needed.
- **Border (#E5E7EB):** The structural line color for low-contrast separation in outlined components.
- **Success (#7EE24B):** The vivid lime accent used for the brand mark and focal decorative elements. It introduces personality and movement.
- **Error (#D92D20):** Reserved for destructive or invalid states; it is not visually dominant in the landing-page composition.

## Typography
Saans is the sole type family, giving the system a modern, slightly rounded grotesk character. Headlines are large, confident, and tightly set, with the display style using a bold 652 weight and negative letter spacing to create a compact, premium wordmark feel. Body copy stays lightweight and highly readable, while labels and button text lean semibold for clarity.

The hierarchy is simple and expressive: `headline-display` for hero messaging, `headline-lg` through `headline-sm` for supporting page structure, and `body-lg`/`body-md` for descriptive text. Labels use strong, compact weights for CTAs, navigation, and chips. There is no visible uppercase convention; instead, the system relies on weight and spacing rather than all-caps styling.

## Layout & Spacing
The page is centered and highly symmetrical, with a fixed-feeling hero column that floats in a wide viewport. Spacing is intentionally expansive: the scale jumps from 8px to 24px, 40px, 80px, and 120px, which supports a calm, luxury-grade rhythm rather than dense utility layout. Sections breathe heavily, and content blocks are separated by large vertical gaps to preserve focus.

Containers and components favor soft alignment over complex grid structure. Buttons, nav items, and logos are grouped with plenty of margin, and cards or panels should keep generous interior padding so the white space remains part of the composition. Use the larger spacing steps for section breaks and the smaller steps for component internals.

## Elevation & Depth
The visual system is intentionally flat. Instead of layered shadows, it uses contrast, borders, and whitespace to create hierarchy. The only notable depth cue is a very subtle inset line used on outlined controls and some button treatments, which adds definition without looking raised.

Surface separation should come from border-gray outlines and tonal shifts rather than heavy blur or shadow stacks. Avoid dramatic elevation unless a component truly needs emphasis; the baseline language is calm, crisp, and almost paper-like.

## Shapes
The shape language is soft and friendly, with strong use of pill forms for key interactive elements. Primary and secondary buttons, the top navigation container, and the input pattern all favor `rounded.full`, while cards use a modest `rounded.md` to keep structure clean. The overall feel is approachable and polished, not angular or technical.

Small-radius surfaces should stay understated, while the most important CTAs can become fully rounded to signal action and reduce friction. The system avoids sharp corners except where a precise, link-like treatment is needed.

## Components
Buttons are the most expressive component in the system. `button-primary` is a filled black pill with white text, used for the main conversion action. `button-secondary` is a white pill with dark text and a subtle outline/inset treatment, ideal for adjacent supporting actions. `button-tertiary` is text-only and should be reserved for low-emphasis navigation or helper links. Keep buttons at a minimum height of 44px with compact horizontal padding so they feel confident but not bulky.

Cards should use `card` with a white surface, light border, and `rounded.md`. They should remain understated, with 16px padding and no heavy shadow. Inputs should follow the same visual language as secondary buttons: pill-shaped, white, bordered, and easy to scan.

Navigation items should be quiet and compact, matching the site’s restrained header. Logo treatments may be bold and dark, but they should not introduce extra chroma outside the green brand accent. If chips, badges, or filters are added, they should mirror the button and input language: simple outlines, low elevation, and rounded full or near-full corners.

## Do's and Don'ts
- Do keep layouts centered, spacious, and visually quiet so the hero remains the focus.
- Do use Saans consistently across headlines, body copy, labels, and controls.
- Do rely on black, white, and soft gray for most UI, with the lime accent used sparingly.
- Do favor pill-shaped CTAs and subtle bordered surfaces over heavy shadows.
- Don't introduce multiple loud accent colors or gradient treatments.
- Don't compress spacing or create dense, dashboard-like layouts.
- Don't use sharp corners or ornate decoration on core components.
- Don't add strong elevation unless the design truly needs separation.