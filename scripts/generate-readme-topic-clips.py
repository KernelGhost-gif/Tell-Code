#!/usr/bin/env python3
"""Generate 13 seamless five-second looping animations for the Tell README."""
from __future__ import annotations
import math, subprocess, tempfile
from functools import lru_cache
from pathlib import Path
import numpy as np
from PIL import Image, ImageDraw, ImageFont

W, H, FPS, DURATION = 960, 360, 10, 5
FRAMES = FPS * DURATION
BG0=(5,13,25); BG1=(8,20,37); PANEL=(12,28,49)
STROKE=(29,57,86); TEXT=(236,244,255); MUTED=(139,164,194)
CYAN=(92,218,255); BLUE=(107,139,255); VIOLET=(181,145,255)
GREEN=(116,230,190); AMBER=(255,194,117); RED=(255,125,139)
FONT_REG='/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'
FONT_BOLD='/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf'
FONT_MONO='/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf'

TOPICS = [
 ('intro','Code should explain itself','Readable syntax. Real execution.','SOURCE → STRUCTURE → OUTPUT',CYAN),
 ('why-tell','Why Tell?','Clarity without giving up language depth.','DESIGNED TO READ. BUILT TO RUN.',VIOLET),
 ('quick-start','Quick start','Download it. Run it. Make a project.','THREE STEPS TO YOUR FIRST PROGRAM',GREEN),
 ('language','The language','Sentence-shaped statements, familiar logic.','A SMALL LANGUAGE WITH REAL STRUCTURE',CYAN),
 ('features','What you get','Core language, data, objects, modules and tools.','THE TOOLBOX IS ALREADY HERE',BLUE),
 ('execution','How it works','One source language. Multiple execution paths.','INTERPRETER · BYTECODE · JIT',VIOLET),
 ('toolchain','The wider Tell toolchain','Runtime, compiler, editor and compatibility path.','FOUR PROJECTS. CLEAR BOUNDARIES.',AMBER),
 ('examples','Examples worth opening','Start small, then explore real language features.','LEARN BY READING RUNNABLE CODE',GREEN),
 ('documentation','Documentation','Specifications and focused guides for each layer.','REFERENCE THE ACTUAL IMPLEMENTATION',CYAN),
 ('build-test','Build and test','Build the runtime and verify the whole project.','GO TEST · VET · CROSS-BUILD',BLUE),
 ('status','Project status','Active development, with limits stated clearly.','WHAT WORKS. WHAT IS NEXT.',AMBER),
 ('contributing','Contributing','Ideas, examples, tests and code all help.','GOOD CHANGES COME WITH TESTS',GREEN),
 ('license','License','MIT-licensed and open to use.','OPEN SOURCE. CLEAR TERMS.',VIOLET),
]

@lru_cache(maxsize=128)
def font(size, bold=False, mono=False):
    return ImageFont.truetype(FONT_MONO if mono else FONT_BOLD if bold else FONT_REG, size)

def txt(d, xy, s, size=16, fill=TEXT, bold=False, mono=False, anchor=None):
    d.text(xy, s, font=font(size,bold,mono), fill=fill, anchor=anchor)

def rr(d, box, radius=14, fill=PANEL, outline=STROKE, width=1):
    d.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)

def line(d, pts, fill=STROKE, width=2):
    d.line(pts, fill=fill, width=width, joint='curve')

def dot(d,x,y,r,c):
    d.ellipse((x-r,y-r,x+r,y+r),fill=c)

_Y = np.linspace(0.0, 1.0, H, dtype=np.float32)[:, None, None]
_BG_ARRAY = np.asarray(BG0, dtype=np.float32)[None, None, :] * (1.0-_Y) + np.asarray(BG1, dtype=np.float32)[None, None, :] * _Y
_BG_ARRAY = np.broadcast_to(_BG_ARRAY, (H, W, 3)).astype(np.uint8).copy()
_STATIC_BG = Image.fromarray(_BG_ARRAY, 'RGB')
_static_draw = ImageDraw.Draw(_STATIC_BG, 'RGBA')
for _x in range(24, W, 32): _static_draw.line((_x,0,_x,H), fill=(38,72,108,24), width=1)
for _y in range(8, H, 32): _static_draw.line((0,_y,W,_y), fill=(38,72,108,24), width=1)

def base(topic, phase):
    key,title,subtitle,kicker,accent=topic
    im=_STATIC_BG.copy()
    d=ImageDraw.Draw(im,'RGBA')
    for i in range(24):
        xx=500 + (i*83)%430 + math.sin(phase*2*math.pi + i)*4
        yy=18 + (i*47)%318 + math.cos(phase*2*math.pi+i*.7)*3
        d.ellipse((xx,yy,xx+2,yy+2), fill=(*accent,45))
    rr(d,(42,38,166,66),14,fill=(11,33,55,255),outline=(*accent,100))
    dot(d,58,52,4,accent)
    txt(d,(71,43),'TELL  /  0.3.8',12,TEXT,True)
    words=title.split(); rows=[]; cur=''
    for word in words:
        nxt=(cur+' '+word).strip()
        if d.textbbox((0,0),nxt,font=font(29,True))[2] > 310 and cur:
            rows.append(cur); cur=word
        else: cur=nxt
    if cur: rows.append(cur)
    yy=96
    for row in rows[:2]:
        txt(d,(42,yy),row,29,TEXT,True); yy+=38
    subfont=font(14); subrows=[]; subcur=''
    for word in subtitle.split():
        candidate=(subcur+' '+word).strip()
        if d.textbbox((0,0),candidate,font=subfont)[2] > 310 and subcur:
            subrows.append(subcur); subcur=word
        else: subcur=candidate
    if subcur: subrows.append(subcur)
    for j,row in enumerate(subrows[:2]): txt(d,(44,yy+13+j*18),row,14,MUTED)
    d.rounded_rectangle((44,yy+53,89,yy+57),radius=2,fill=accent)
    txt(d,(44,yy+72),kicker,10,accent,True)
    d.line((42,326,918,326), fill=(49,77,111,120), width=1)
    txt(d,(44,336),'TELL  ·  READABLE CODE, REAL RUNTIME',9,(111,143,177))
    txt(d,(916,336),'LOOP  /  05.0s',9,(111,143,177),anchor='ra')
    return im

def scene_intro(d,p,a):
    rr(d,(370,62,652,294),17,fill=(9,23,41),outline=(48,83,119))
    for i,c in enumerate((RED,AMBER,GREEN)): dot(d,389+i*17,82,4,c)
    txt(d,(432,73),'hello.tell',11,MUTED,mono=True)
    rows=[('Let','developer','be','"Ada".'),('Let','project','be','"something wonderful".'),('Say','"Hello, {developer}."','',''),('Say',"\"Let's build {project}.\"",'','')]
    y=119
    for i,(a1,a2,a3,a4) in enumerate(rows):
        x=390
        for s,c in ((a1,CYAN),(a2,VIOLET),(a3,MUTED),(a4,GREEN)):
            if s: txt(d,(x,y),s,11,c,mono=True); x+=d.textbbox((0,0),s,font=font(11,False,True))[2]+7
        y+=37
    cx=390 + (math.sin(2*math.pi*p)*.5+.5)*230
    d.rounded_rectangle((cx,253,cx+2,270),radius=1,fill=(*CYAN,220))
    line(d,[(662,176),(700,176)],(61,105,144),3)
    pulse=.5+.5*math.sin(2*math.pi*3*p)
    dot(d,666+pulse*30,176,4,CYAN)
    rr(d,(710,101,919,255),17,fill=(10,29,47),outline=(55,105,130))
    txt(d,(730,120),'OUTPUT',10,GREEN,True)
    txt(d,(730,159),'Hello, Ada.',15,TEXT,True)
    txt(d,(730,192),"Let's build",13,MUTED)
    txt(d,(730,214),'something wonderful.',13,MUTED)
    dot(d,889,123,4,GREEN)

def scene_why(d,p,a):
    items=[('Readable by design','Intent stays visible',CYAN),('Real language','Functions · modules · errors',VIOLET),('Native execution','Standalone Go runtime',GREEN),('Multiple paths','Interpreter · bytecode · JIT',AMBER)]
    for i,(h,s,c) in enumerate(items):
        y=63+i*59
        rr(d,(370,y,678,y+46),12,fill=(10,26,45),outline=(*c,95))
        dot(d,391,y+23,6,c); txt(d,(408,y+7),h,13,TEXT,True); txt(d,(408,y+26),s,10,MUTED)
    rr(d,(718,111,922,230),17,fill=(12,33,54),outline=(*CYAN,115),width=2)
    txt(d,(820,137),'TELL',24,TEXT,True,anchor='mm')
    txt(d,(820,171),'ONE LANGUAGE',10,CYAN,True,anchor='mm')
    txt(d,(820,194),'CLEAR INTENT',10,MUTED,anchor='mm')
    for i,c in enumerate((CYAN,VIOLET,GREEN,AMBER)):
        ang=2*math.pi*(p+i/4)
        dot(d,820+math.cos(ang)*112,170+math.sin(ang)*75,3,c)

def scene_quick(d,p,a):
    labels=[('01','GET THE BINARY','tell --version',CYAN),('02','TRY THE CLI','tell examples/hello.tell',GREEN),('03','MAKE A PROJECT','tell new hello-world',VIOLET)]
    for i,(n,h,cmd,c) in enumerate(labels):
        x=366+i*188
        rr(d,(x,83,x+168,253),15,fill=(10,27,47),outline=(*c,110))
        rr(d,(x+13,98,x+47,122),8,fill=(*c,35),outline=(*c,120))
        txt(d,(x+30,102),n,11,c,True,anchor='mm')
        txt(d,(x+13,142),h,10,TEXT,True)
        rr(d,(x+12,166,x+156,222),8,fill=(5,15,28),outline=(34,62,92))
        disp=cmd if len(cmd)<20 else cmd[:18]+'…'
        txt(d,(x+19,184),'> '+disp,9,GREEN,mono=True)
        txt(d,(x+19,204),'ready',9,MUTED,mono=True)
        d.rounded_rectangle((x+13,237,x+155,241),radius=2,fill=(28,52,78))
        bar=.5+.5*math.sin(2*math.pi*p+i*2*math.pi/3)
        d.rounded_rectangle((x+13,237,x+13+142*bar,241),radius=2,fill=c)
    for i in range(2):
        x=540+i*188
        dot(d,x,168,3,(110,146,180))

def scene_language(d,p,a):
    cards=[('DECLARE','Let score be 10.',CYAN),('DECIDE','If ready:',VIOLET),('ITERATE','For each item:',GREEN),('DEFINE','To greet(name):',AMBER)]
    for i,(h,s,c) in enumerate(cards):
        x=370+(i%2)*273; y=74+(i//2)*112
        rr(d,(x,y,x+251,y+87),13,fill=(10,27,46),outline=(*c,105))
        txt(d,(x+17,y+15),h,10,c,True)
        txt(d,(x+17,y+47),s,13,TEXT,mono=True)
    rr(d,(370,305,894,313),4,fill=(22,48,75),outline=(22,48,75))
    d.rounded_rectangle((370,305,370+524*(.5+.5*math.sin(2*math.pi*p)),313),radius=4,fill=CYAN)
    txt(d,(913,302),'GRAMMAR',9,MUTED,anchor='ra')

def scene_features(d,p,a):
    features=[('CORE LANGUAGE','Branches · loops',CYAN),('DATA','Lists · maps · sets',GREEN),('OBJECTS','Classes · methods',VIOLET),('MODULES','Local imports',AMBER),('TESTS','Checks · diagnostics',BLUE),('CLI','Run · REPL · format',CYAN)]
    for i,(h,s,c) in enumerate(features):
        col=i%3; row=i//3; x=367+col*184; y=78+row*103
        rr(d,(x,y,x+168,y+83),13,fill=(10,27,47),outline=(*c,100))
        gx=x+20; gy=y+25
        if i==0: line(d,[(gx,gy),(gx+12,gy+12),(gx+26,gy-9)],c,3)
        elif i==1:
            for j in range(3): rr(d,(gx+j*8,gy-7,gx+j*8+5,gy+13),2,fill=c,outline=c)
        elif i==2:
            rr(d,(gx,gy-9,gx+26,gy+13),4,outline=c,width=2); dot(d,gx+13,gy+2,3,c)
        elif i==3:
            dot(d,gx+5,gy+1,5,c); dot(d,gx+22,gy+1,5,c); line(d,[(gx+9,gy+1),(gx+18,gy+1)],c,2)
        elif i==4: line(d,[(gx,gy),(gx+8,gy+8),(gx+26,gy-12)],c,3)
        else:
            rr(d,(gx,gy-12,gx+27,gy+14),4,outline=c,width=2); txt(d,(gx+13,gy-9),'>',12,c,True,anchor='ma')
        txt(d,(x+62,y+17),h,9,c,True); txt(d,(x+17,y+55),s,11,TEXT)

def scene_execution(d,p,a):
    rr(d,(365,129,482,207),13,fill=(10,28,47),outline=(67,119,153))
    txt(d,(423,151),'SOURCE',10,CYAN,True,anchor='mm'); txt(d,(423,174),'.tell',12,TEXT,mono=True,anchor='mm')
    rr(d,(515,129,637,207),13,fill=(10,28,47),outline=(67,119,153))
    txt(d,(576,151),'PARSER',10,VIOLET,True,anchor='mm'); txt(d,(576,174),'AST',14,TEXT,True,anchor='mm')
    line(d,[(482,168),(515,168)],(71,115,149),3)
    pulse=.5+.5*math.sin(2*math.pi*3*p); dot(d,483+pulse*30,168,4,CYAN)
    line(d,[(637,168),(669,168),(669,89),(704,89)],(67,115,149),2)
    line(d,[(637,168),(704,168)],(67,115,149),2)
    line(d,[(637,168),(669,168),(669,247),(704,247)],(67,115,149),2)
    outputs=[('INTERPRETER','default path',GREEN,89),('BYTECODE VM','supported subset',BLUE,168),('NATIVE JIT','eligible hot code',VIOLET,247)]
    for h,s,c,y in outputs:
        rr(d,(704,y-27,916,y+27),11,fill=(10,28,47),outline=(*c,130))
        dot(d,720,y,4,c); txt(d,(734,y-13),h,10,TEXT,True); txt(d,(734,y+5),s,9,MUTED)
    dot(d,681,168+79*math.sin(2*math.pi*p),4,AMBER)

def scene_toolchain(d,p,a):
    entries=[('Tell','GO','Native runtime',CYAN),('TellC','LLVM','Experimental compiler',VIOLET),('Tell Studio','RUST','Early desktop editor',GREEN),('Python','LEGACY','Compatibility runtime',AMBER)]
    for i,(name,tag,desc,c) in enumerate(entries):
        x=366+(i%2)*275; y=72+(i//2)*116
        rr(d,(x,y,x+250,y+94),14,fill=(10,27,47),outline=(*c,110))
        rr(d,(x+15,y+15,x+65,y+39),7,fill=(*c,35),outline=(*c,110))
        txt(d,(x+40,y+20),tag,9,c,True,anchor='mm')
        txt(d,(x+80,y+15),name,15,TEXT,True); txt(d,(x+17,y+57),desc,10,MUTED)
        dot(d,x+226,y+72,3,c)

def scene_examples(d,p,a):
    cards=[('hello.tell','Say "Hello, world."',CYAN),('features.tell','For each value in values:',GREEN),('factorial.tell','Give back n times factorial(n - 1).',VIOLET)]
    for i,(name,code,c) in enumerate(cards):
        x=366+i*186
        rr(d,(x,90,x+169,253),13,fill=(9,24,42),outline=(*c,100))
        dot(d,x+16,108,3,RED); dot(d,x+27,108,3,AMBER); dot(d,x+38,108,3,GREEN)
        txt(d,(x+15,129),name,10,MUTED,mono=True)
        if i==0: lines=['Say "Hello,','world."']
        elif i==1: lines=['For each value','in values:','    Say value.']
        else: lines=['Give back n *','factorial(n - 1).']
        for j,s in enumerate(lines): txt(d,(x+14,164+j*22),s,9,c if j==0 else TEXT,mono=True)
        d.rounded_rectangle((x+14,229,x+154,233),2,fill=(28,50,74))
        pos=.5+.5*math.sin(2*math.pi*p+i*2*math.pi/3)
        d.rounded_rectangle((x+14,229,x+14+140*pos,233),2,fill=c)

def scene_documentation(d,p,a):
    docs=[('LANGUAGE SPEC','Grammar & portable core',CYAN),('CLI REFERENCE','Commands & flags',GREEN),('EXECUTION ENGINES','VM, JIT & fallbacks',VIOLET),('SECURITY','Effects & limitations',AMBER)]
    for i,(h,s,c) in enumerate(docs):
        x=366+(i%2)*276; y=75+(i//2)*111
        rr(d,(x,y,x+252,y+89),12,fill=(10,26,45),outline=(*c,100))
        rr(d,(x+15,y+18,x+43,y+54),4,fill=(16,43,66),outline=(*c,155))
        for k in range(3): d.line((x+21,y+27+k*8,x+37-(k%2)*5,y+27+k*8),fill=(*c,170),width=2)
        txt(d,(x+58,y+19),h,10,c,True); txt(d,(x+58,y+45),s,10,TEXT)
        d.rounded_rectangle((x+58,y+68,x+226,y+72),2,fill=(26,51,77))
        d.rounded_rectangle((x+58,y+68,x+58+168*(.65+.2*math.sin(2*math.pi*p+i)),y+72),2,fill=(*c,180))

def scene_build_test(d,p,a):
    stages=[('BUILD','go build',CYAN),('TEST','go test ./...',GREEN),('VET','go vet ./...',VIOLET),('PACKAGE','6 targets',AMBER)]
    for i,(h,s,c) in enumerate(stages):
        x=365+i*139
        rr(d,(x,113,x+119,222),12,fill=(10,27,46),outline=(*c,110))
        txt(d,(x+59,139),h,10,c,True,anchor='mm'); txt(d,(x+59,166),s,9,TEXT,mono=True,anchor='mm')
        pulse=(math.sin(2*math.pi*p-i*.9)+1)/2
        dot(d,x+59,197,7,c); d.ellipse((x+51,189,x+67,205),outline=(*c,int(70+130*pulse)),width=2)
        if i<3: line(d,[(x+119,168),(x+139,168)],(60,97,132),2)
    rr(d,(408,259,838,293),10,fill=(9,26,43),outline=(52,112,100))
    txt(d,(623,276),'CHECKSUMS  ✓     SBOM  ✓     NOTICES  ✓',10,GREEN,True,anchor='mm')

def scene_status(d,p,a):
    blocks=[('GO RUNTIME','Active development',GREEN),('BYTECODE VM','Supported subset',BLUE),('NATIVE JIT','Narrow numeric tier',VIOLET),('PACKAGE MANAGER','Not implemented',AMBER)]
    for i,(h,s,c) in enumerate(blocks):
        y=73+i*58
        txt(d,(372,y+2),h,10,c,True); txt(d,(550,y+2),s,11,TEXT)
        rr(d,(372,y+28,910,y+33),3,fill=(24,47,72),outline=(24,47,72))
        w=[480,340,190,18][i]
        d.rounded_rectangle((372,y+28,372+w,y+33),radius=3,fill=c)
    d.rounded_rectangle((360,66,918,302),radius=14,outline=(75,107,144,45),width=1)

def scene_contributing(d,p,a):
    nodes=[('IDEA',CYAN),('CHANGE',BLUE),('TESTS',GREEN),('PULL REQUEST',VIOLET)]
    xs=[385,535,685,835]; y=166
    for i,(h,c) in enumerate(nodes):
        rr(d,(xs[i]-52,y-34,xs[i]+52,y+34),12,fill=(10,27,46),outline=(*c,115))
        dot(d,xs[i],y-9,5,c); txt(d,(xs[i],y+10),h,9,TEXT,True,anchor='mm')
        if i<3:
            line(d,[(xs[i]+53,y),(xs[i+1]-53,y)],(60,97,133),2)
            q=.5+.5*math.sin(2*math.pi*p+i*2*math.pi/3)
            dot(d,xs[i]+53+(xs[i+1]-xs[i]-106)*q,y,3,CYAN)
    rr(d,(470,242,750,283),11,fill=(10,28,45),outline=(52,99,91))
    txt(d,(610,262),'SPEC  →  TESTS  →  REVIEW',11,GREEN,True,anchor='mm')

def scene_license(d,p,a):
    rr(d,(430,72,708,288),17,fill=(10,27,47),outline=(*VIOLET,120),width=2)
    rr(d,(455,95,683,130),9,fill=(19,43,69),outline=(60,92,132))
    txt(d,(569,112),'MIT LICENSE',17,TEXT,True,anchor='mm')
    for i,w in enumerate((184,204,163,191,126)):
        yy=157+i*19
        d.rounded_rectangle((459,yy,459+w,yy+4),2,fill=(86,117,151,150))
    cx,cy=789,178; r=59+3*math.sin(2*math.pi*p)
    d.ellipse((cx-r,cy-r,cx+r,cy+r),outline=(*CYAN,190),width=2)
    d.ellipse((cx-43,cy-43,cx+43,cy+43),fill=(12,43,64),outline=(*CYAN,90),width=1)
    txt(d,(cx,cy-5),'OPEN',17,TEXT,True,anchor='mm'); txt(d,(cx,cy+17),'SOURCE',9,CYAN,True,anchor='mm')
    dot(d,cx+math.cos(2*math.pi*p)*r,cy+math.sin(2*math.pi*p)*r,4,AMBER)

SCENES={
 'intro':scene_intro,'why-tell':scene_why,'quick-start':scene_quick,'language':scene_language,
 'features':scene_features,'execution':scene_execution,'toolchain':scene_toolchain,'examples':scene_examples,
 'documentation':scene_documentation,'build-test':scene_build_test,'status':scene_status,
 'contributing':scene_contributing,'license':scene_license,
}

def render(topic, outdir):
    key=topic[0]; frames=[]
    for i in range(FRAMES):
        phase=i/FRAMES
        im=base(topic,phase)
        d=ImageDraw.Draw(im,'RGBA')
        SCENES[key](d,phase,topic[4])
        frames.append(im)
    gif=outdir/f'{key}.gif'
    small=[f.resize((720,270),Image.Resampling.LANCZOS) for f in frames]
    pal=small[0].quantize(colors=64,method=Image.Quantize.MEDIANCUT)
    pframes=[small[0].quantize(palette=pal,dither=Image.Dither.NONE)]
    for f in small[1:]: pframes.append(f.quantize(palette=pal,dither=Image.Dither.NONE))
    pframes[0].save(gif,save_all=True,append_images=pframes[1:],duration=round(1000/FPS),loop=0,optimize=True,disposal=1)
    with tempfile.TemporaryDirectory() as tmp:
        for i,im in enumerate(frames): im.save(Path(tmp)/f'{i:04d}.png')
        mp4=outdir/f'{key}.mp4'
        subprocess.run(['ffmpeg','-loglevel','error','-y','-framerate',str(FPS),'-i',str(Path(tmp)/'%04d.png'),'-c:v','libx264','-preset','ultrafast','-crf','24','-pix_fmt','yuv420p','-movflags','+faststart','-an',str(mp4)],check=True)
    return gif.stat().st_size, mp4.stat().st_size

def update_readme(readme_path, outdir):
    source=readme_path.read_text(encoding='utf-8')
    for key,title,subtitle,kicker,accent in TOPICS:
        heading=f'## {title}'
        tag=f'<!-- tell-loop:{key} -->'
        embed=(f'\n\n{tag}\n<p align="center">\n'
               f'  <img src="assets/tell-clips/{key}.gif" alt="{title} — five-second looping animation" width="100%">\n'
               f'</p>\n<p align="right"><sub><a href="assets/tell-clips/{key}.mp4">MP4 clip</a></sub></p>\n')
        if heading not in source: raise SystemExit(f'Missing expected README heading: {heading}')
        if tag not in source: source=source.replace(heading,heading+embed,1)
    readme_path.write_text(source,encoding='utf-8')

if __name__=='__main__':
    import argparse
    ap=argparse.ArgumentParser()
    ap.add_argument('--out',default='assets/tell-clips')
    ap.add_argument('--readme',default='README.md')
    ap.add_argument('--update-readme',action='store_true')
    args=ap.parse_args()
    out=Path(args.out); out.mkdir(parents=True,exist_ok=True)
    total_gif=total_mp4=0
    for topic in TOPICS:
        gs,ms=render(topic,out); total_gif+=gs; total_mp4+=ms
        print(f'{topic[0]:16s} GIF {gs/1024:7.1f} KiB  MP4 {ms/1024:7.1f} KiB')
    if args.update_readme: update_readme(Path(args.readme),out)
    print(f'TOTAL GIF {total_gif/1024:.1f} KiB | MP4 {total_mp4/1024:.1f} KiB')
