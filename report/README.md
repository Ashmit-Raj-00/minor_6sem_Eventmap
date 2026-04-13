# EventMap Minor Report Assets

This folder contains the Minor Project report (`report/Minor_Project_Report_EventMap.md`) and Mermaid diagram sources under `report/diagrams/`.

## How to use
- Edit placeholders like `<<Student Name>>`, `<<Guide Name>>`, etc. in `report/Minor_Project_Report_EventMap.md`.
- If your final submission requires images instead of Mermaid code blocks, export the `.mmd` files from `report/diagrams/` as PNG/SVG and paste them into your Word/PDF.

## Diagram export (optional)
If you have Node.js installed, one common approach:
1. Install Mermaid CLI: `npm i -g @mermaid-js/mermaid-cli`
2. Export: `mmdc -i report/diagrams/er-diagram.mmd -o er-diagram.png`

You can also paste the diagram text into any Mermaid renderer to export as an image.

