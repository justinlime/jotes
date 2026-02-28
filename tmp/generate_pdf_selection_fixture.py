from pathlib import Path

root = Path("/home/justinlime/code/jotes/tmp")
content_stream = b"BT\n/F1 24 Tf\n72 100 Td\n(Hello copyable PDF text) Tj\nET\n"
objects = [
    b"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
    b"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
    b"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n",
    b"4 0 obj\n<< /Length " + str(len(content_stream)).encode("ascii") + b" >>\nstream\n" + content_stream + b"endstream\nendobj\n",
    b"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
]

pdf_bytes = bytearray(b"%PDF-1.4\n")
offsets = [0]
for obj in objects:
    offsets.append(len(pdf_bytes))
    pdf_bytes.extend(obj)

xref_start = len(pdf_bytes)
pdf_bytes.extend(f"xref\n0 {len(objects) + 1}\n".encode("ascii"))
pdf_bytes.extend(b"0000000000 65535 f \n")
for offset in offsets[1:]:
    pdf_bytes.extend(f"{offset:010d} 00000 n \n".encode("ascii"))
pdf_bytes.extend(
    b"trailer\n"
    + f"<< /Size {len(objects) + 1} /Root 1 0 R >>\n".encode("ascii")
    + b"startxref\n"
    + f"{xref_start}\n".encode("ascii")
    + b"%%EOF\n"
)

(root / "pdf-selection-test.pdf").write_bytes(pdf_bytes)
