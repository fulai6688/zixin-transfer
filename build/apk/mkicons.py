import os, sys
from PIL import Image
root = sys.argv[1]
master = Image.open(sys.argv[2]).convert("RGBA")
sizes = {"mdpi":48, "hdpi":72, "xhdpi":96, "xxhdpi":144, "xxxhdpi":192}
for dpi, px in sizes.items():
    d = os.path.join(root, "android", "res", "mipmap-" + dpi)
    os.makedirs(d, exist_ok=True)
    master.resize((px, px), Image.LANCZOS).save(os.path.join(d, "ic_launcher.png"))
    # 圆形/自适应风格留白版本（部分启动器喜欢）
print("icons ok")