#!/bin/sh
set -eu

ROOT="${1:-.}"

# This script is intended to be used as a pre-commit hook.
# It ensures that every Go type defining:
#   func (T) ObjectType() string
# is registered somewhere via:
#   astral.Add(&T{})
#
# SECURITY:
# - Never expands file lists through the shell
# - Uses find -print0 + xargs -0 to handle all filenames safely
# - Uses `--` to prevent option injection for perl/grep

# Fast “no files” exit (POSIX-safe)
if ! find "$ROOT" -type f -name "*.go" ! -path "*/vendor/*" ! -path "*/.*/*" -print -quit 2>/dev/null | grep -q .; then
  echo "No Go files found under $ROOT"
  exit 0
fi

# ---------- Collect ObjectType implementers ----------
# Rule: any type that defines ObjectType() string must be registered.
# Ignore rule: skip if the immediately preceding line contains // astral:blueprint-ignore

OBJECT_TYPES=$(
  find "$ROOT" \
    -type f -name "*.go" \
    ! -path "*/vendor/*" \
    ! -path "*/.*/*" \
    -print0 \
  | xargs -0 perl -0777 -ne '
      # For each ObjectType method signature, extract receiver type name.
      while (m/^[[:space:]]*func[[:space:]]*\(([^)]*)\)[[:space:]]*ObjectType[[:space:]]*\(\)[[:space:]]*string/gm) {
        my $recv = $1;

        # Check the immediately preceding line for ignore comment.
        my $start = $-[0];
        my $pre = substr($_, 0, $start);
        my $prevline = "";
        if ($pre =~ /(?:\n|\A)([^\n]*)\n\z/s) {
          $prevline = $1;
        } elsif ($pre =~ /([^\n]*)\z/s) {
          $prevline = $1;
        }
        next if $prevline =~ /\/\/\s*astral:blueprint-ignore/;

        # Normalize receiver: "(T)", "(t T)", "(*T)", "(t *T)", "(t pkg.T)", "(t *pkg.T)"
        $recv =~ s/^[[:space:]]+|[[:space:]]+$//g;
        $recv =~ s/.*[[:space:]]+\*?//; # drop receiver var if present
        $recv =~ s/^\*//;               # drop leading *
        $recv =~ s/.*\.//;              # drop package qualifier

        print "$recv\n" if $recv =~ /^[A-Za-z_][A-Za-z0-9_]*$/;
      }
    ' -- \
  | sort -u
)

# ---------- Collect Blueprint registrations ----------
# Accept:
#   astral.Add(&T{})
#   astral.MustAdd(&T{})
#   Add(&T{})
#   var v T; ... Add(&v)
# Supports multiline because perl is slurping (-0777).

BLUEPRINT_TYPES=$(
  find "$ROOT" \
    -type f -name "*.go" \
    ! -path "*/vendor/*" \
    ! -path "*/.*/*" \
    -print0 \
  | xargs -0 perl -0777 -ne '
      # Match both old and new patterns:
      # Old: astral.DefaultBlueprints.Add(&T{})
      # New: astral.Add(&T{}) or Add(&T{})

      # Pattern 1: astral.Add(&Type{}), astral.MustAdd(&Type{}) or pkg.Add(&Type{})
      while (/(?:^|\W)(?:[A-Za-z_][A-Za-z0-9_]*\.)?(?:Must)?Add\s*\(\s*&\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\s*(?:\{|\))/gms) {
        my $t = $1;
        $t =~ s/.*\.//; # drop package qualifier
        print "$t\n";
      }

      # Pattern 2: var v Type; ... Add(&v)
      while (/var\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\s*.*?(?:[A-Za-z_][A-Za-z0-9_]*\.)?(?:Must)?Add\s*\(\s*&\s*\1\s*\)/gms) {
        my $t = $2;
        $t =~ s/.*\.//; # drop package qualifier
        print "$t\n";
      }
    ' -- \
  | sort -u
)

FAILED=0

# ---------- Compare ----------
for t in $OBJECT_TYPES; do
  if ! printf '%s\n' "$BLUEPRINT_TYPES" | grep -qx "$t"; then
    echo "ERROR: Astral object type '$t' defines ObjectType() string but is not registered via Add(&${t}{...})"
    echo "  Defined at:"

    # Safe file listing: NUL-delimited + xargs -0 + grep -- to prevent option injection
    find "$ROOT" \
      -type f -name "*.go" \
      ! -path "*/vendor/*" \
      ! -path "*/.*/*" \
      -print0 \
    | xargs -0 grep -nH -E '^[[:space:]]*func[[:space:]]*\([^)]*\)[[:space:]]*ObjectType[[:space:]]*\(\)[[:space:]]*string' -- \
    | grep -E "\(\s*([A-Za-z_][A-Za-z0-9_]*\s+)?\*?([A-Za-z_][A-Za-z0-9_]*\.)?${t}\s*\)" \
    | sed -E 's/:([0-9]+):.*/:\1/' \
    | sed -E 's/^/  at /'

    FAILED=1
  fi
done

if [ "$FAILED" -ne 0 ]; then
  echo
  echo "Blueprint registration check FAILED."
  echo
  echo "Hint: add in an init() function:"
  echo "  _ = astral.Add(&<Type>{})"
  exit 1
fi

echo "Blueprint registration check passed."
