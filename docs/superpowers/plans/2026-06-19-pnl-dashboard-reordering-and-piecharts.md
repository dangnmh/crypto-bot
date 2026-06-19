# P&L Dashboard Reordering and Pie Chart Overrides Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorder the 23 panels on the P&L dashboard into a gapless grid and apply categorical color overrides to the remaining pie charts.

**Architecture:** We will implement a Python verification script to programmatically detect panel overlapping and validate layout boundaries, then modify the Grafana dashboard JSON configuration.

**Tech Stack:** Grafana Dashboard JSON, Python (for validation)

## Global Constraints
- No panels should overlap.
- The maximum width for any row configuration is 24 units.
- All JSON changes must result in valid JSON.

---

### Task 1: Create Layout and Override Validator

**Files:**
- Create: `scripts/validate_dashboard.py`

**Interfaces:**
- Consumes: None
- Produces: Command-line exit code 0 if the dashboard configuration is valid, non-zero otherwise.

- [ ] **Step 1: Write the validation script**
  Create the file `scripts/validate_dashboard.py` with the following content:

```python
import json
import sys

def main():
    path = "deploy/grafana/dashboards/pnl-analytics.json"
    try:
        with open(path, "r") as f:
            data = json.load(f)
    except Exception as e:
        print(f"FAIL: Could not load JSON: {e}")
        sys.exit(1)
        
    panels = data.get("panels", [])
    print(f"Loaded {len(panels)} panels.")
    
    # 1. Check for overlapping panels
    # We will build a grid map of y-levels, representing 24 columns wide.
    # Since y can be arbitrarily high, we'll keep track of used coordinates.
    used_cells = set()
    errors = []
    
    for i, p in enumerate(panels):
        title = p.get("title", f"Panel ID {p.get('id')}")
        grid = p.get("gridPos", {})
        x = grid.get("x", 0)
        y = grid.get("y", 0)
        w = grid.get("w", 0)
        h = grid.get("h", 0)
        
        # Check boundary constraints
        if x < 0 or x + w > 24:
            errors.append(f"Panel '{title}' exceeds grid width boundary: x={x}, w={w}")
        if w <= 0 or h <= 0:
            errors.append(f"Panel '{title}' has invalid size: w={w}, h={h}")
            
        # Check for overlaps
        for row in range(y, y + h):
            for col in range(x, x + w):
                cell = (col, row)
                if cell in used_cells:
                    errors.append(f"Panel '{title}' overlaps at cell (col={col}, row={row})")
                used_cells.add(cell)
                
    # 2. Check Pie Chart overrides
    expected_overrides = {
        26: {"Win": "green", "Loss": "red"},
        14: {"filled": "green", "partial_filled": "yellow", "canceled_no_fill": "blue", "unknown": "red"},
        17: {"target": "green", "stop_loss": "red", "timeout": "orange", "force_close": "dark-red"},
        18: {"ioc_canceled_no_position": "blue", "ioc_outcome_unknown_no_position": "purple"}
    }
    
    for p in panels:
        pid = p.get("id")
        if pid in expected_overrides:
            title = p.get("title")
            overrides = p.get("fieldConfig", {}).get("overrides", [])
            actual_map = {}
            for o in overrides:
                opt = o.get("matcher", {}).get("options")
                val = ""
                for prop in o.get("properties", []):
                    if prop.get("id") == "color":
                        val = prop.get("value", {}).get("fixedColor")
                if opt and val:
                    actual_map[opt] = val
                    
            for key, expected_color in expected_overrides[pid].items():
                if key not in actual_map:
                    errors.append(f"Pie chart '{title}' (ID {pid}) is missing override for label '{key}'")
                elif actual_map[key] != expected_color:
                    errors.append(f"Pie chart '{title}' (ID {pid}) has incorrect color for '{key}': expected '{expected_color}', got '{actual_map[key]}'")

    if errors:
        for err in errors:
            print(f"ERROR: {err}")
        sys.exit(1)
        
    print("SUCCESS: All panels validated correctly, no overlaps found, all overrides match.")
    sys.exit(0)

if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Run verification script**
  Run the validation script. It should fail initially because the layout has gaps/incorrect positioning and does not have the new pie chart overrides.
  Run: `python3 scripts/validate_dashboard.py`
  Expected output: Failure with missing overrides and overlapping/out of boundary errors.

- [ ] **Step 3: Commit**
  ```bash
  git add scripts/validate_dashboard.py
  git commit -m "test: add dashboard validation script"
  ```

---

### Task 2: Reorder Panels and Apply Overrides in Dashboard JSON

**Files:**
- Modify: `deploy/grafana/dashboards/pnl-analytics.json`

**Interfaces:**
- Consumes: `scripts/validate_dashboard.py` from Task 1.
- Produces: A modified `pnl-analytics.json` file.

- [ ] **Step 1: Modify layout gridPos and add fieldConfig overrides**
  Modify [pnl-analytics.json](file:///home/four/projects/crypto-bot/deploy/grafana/dashboards/pnl-analytics.json) to apply the layout configurations specified in the spec and add `fieldConfig.overrides` to the panels with IDs 26, 14, 17, and 18.
  
  For the panel with ID 14 (IOC Order Fill Ratio):
  ```json
  "fieldConfig": {
    "defaults": {
      "custom": {
        "hideFrom": {
          "legend": false,
          "tooltip": false,
          "viz": false
        }
      }
    },
    "overrides": [
      {
        "matcher": {
          "id": "byRegexp",
          "options": "filled"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "green",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "partial_filled"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "yellow",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "canceled_no_fill"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "blue",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "unknown"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "red",
              "mode": "fixed"
            }
          }
        ]
      }
    ]
  }
  ```

  For the panel with ID 17 (Position Exit Reason Breakdown):
  ```json
  "fieldConfig": {
    "defaults": {
      "custom": {
        "hideFrom": {
          "legend": false,
          "tooltip": false,
          "viz": false
        }
      }
    },
    "overrides": [
      {
        "matcher": {
          "id": "byRegexp",
          "options": "target"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "green",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "stop_loss"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "red",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "timeout"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "orange",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "force_close"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "dark-red",
              "mode": "fixed"
            }
          }
        ]
      }
    ]
  }
  ```

  For the panel with ID 18 (Abort Reasons Breakdown):
  ```json
  "fieldConfig": {
    "defaults": {
      "custom": {
        "hideFrom": {
          "legend": false,
          "tooltip": false,
          "viz": false
        }
      }
    },
    "overrides": [
      {
        "matcher": {
          "id": "byRegexp",
          "options": "ioc_canceled_no_position"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "blue",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "ioc_outcome_unknown_no_position"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "purple",
              "mode": "fixed"
            }
          }
        ]
      }
    ]
  }
  ```

  For the panel with ID 26 (Win/Loss Ratio (Completed Trades)):
  ```json
  "fieldConfig": {
    "defaults": {
      "custom": {
        "hideFrom": {
          "legend": false,
          "tooltip": false,
          "viz": false
        }
      }
    },
    "overrides": [
      {
        "matcher": {
          "id": "byRegexp",
          "options": "Win"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "green",
              "mode": "fixed"
            }
          }
        ]
      },
      {
        "matcher": {
          "id": "byRegexp",
          "options": "Loss"
        },
        "properties": [
          {
            "id": "color",
            "value": {
              "fixedColor": "red",
              "mode": "fixed"
            }
          }
        ]
      }
    ]
  }
  ```

- [ ] **Step 2: Run verification script**
  Run: `python3 scripts/validate_dashboard.py`
  Expected: SUCCESS: All panels validated correctly, no overlaps found, all overrides match.

- [ ] **Step 3: Commit**
  ```bash
  git add deploy/grafana/dashboards/pnl-analytics.json
  git commit -m "feat: reorder dashboard panels and apply pie chart color overrides"
  ```
