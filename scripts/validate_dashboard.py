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
