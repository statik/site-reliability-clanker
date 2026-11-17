"""Title sequence for the explainer video, built and rendered by Blender in Python.

A paper boat (the change) sails across dark water through four gates: Plan, Approval,
Grant and Apply. Each gate lights up as the boat passes. Two fish jump along the way.
The camera follows the boat, then the title rises over the water.

Run headless:

    Blender -b -P title.py -- --out <frames dir> [--frames 240] [--fps 30] [--only N]

Everything in the scene is made here; no file is loaded. Eevee, few samples, about half
a second for each 1080p frame.
"""

import argparse
import math
import sys

import bpy

GATES = [(-7.0, "Plan"), (-3.5, "Approval"), (0.0, "Grant"), (3.5, "Apply")]
BOAT_START_X = -10.5
BOAT_END_X = 11.0
SAIL_END_SHARE = 0.72
BLUE = (0.07, 0.25, 1.0, 1.0)
GREEN = (0.07, 0.75, 0.25, 1.0)
PAPER = (0.92, 0.92, 0.9, 1.0)
WATER = (0.004, 0.012, 0.035, 1.0)
SKY = (0.006, 0.008, 0.014, 1.0)


def arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True, help="directory for the PNG frames")
    parser.add_argument("--frames", type=int, default=240)
    parser.add_argument("--fps", type=int, default=30)
    parser.add_argument("--only", type=int, default=0, help="render only this frame (a test)")
    own = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    return parser.parse_args(own)


def material(name, color, roughness=0.6, emission=0.0, metallic=0.0):
    made = bpy.data.materials.new(name)
    made.use_nodes = True
    shader = made.node_tree.nodes["Principled BSDF"]
    shader.inputs["Base Color"].default_value = color
    shader.inputs["Roughness"].default_value = roughness
    shader.inputs["Metallic"].default_value = metallic
    shader.inputs["Emission Color"].default_value = color
    shader.inputs["Emission Strength"].default_value = emission
    return made


def mesh_object(name, vertices, faces, made_material):
    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata(vertices, [], faces)
    mesh.update()
    made = bpy.data.objects.new(name, mesh)
    made.data.materials.append(made_material)
    bpy.context.collection.objects.link(made)
    return made


def paper_boat(paper):
    """A folded paper boat: a hull with pointed ends and the triangular peak in the middle."""
    vertices = [
        (-1.5, 0.0, 0.42), (1.5, 0.0, 0.42),          # 0 bow, 1 stern (top of the hull)
        (-0.6, -0.42, 0.42), (0.6, -0.42, 0.42),      # 2, 3 near rim
        (-0.6, 0.42, 0.42), (0.6, 0.42, 0.42),        # 4, 5 far rim
        (-0.55, 0.0, -0.05), (0.55, 0.0, -0.05),      # 6, 7 keel
        (0.0, 0.0, 1.25),                             # 8 peak
        (-0.5, 0.0, 0.42), (0.5, 0.0, 0.42),          # 9, 10 base of the peak
    ]
    faces = [
        (0, 2, 6), (2, 3, 7, 6), (3, 1, 7),           # near side of the hull
        (0, 6, 4), (4, 6, 7, 5), (5, 7, 1),           # far side of the hull
        (9, 2, 3, 10), (9, 10, 5, 4),                 # deck
        (2, 3, 8), (5, 4, 8), (9, 2, 8), (3, 10, 8), (10, 5, 8), (4, 9, 8),  # the peak
    ]
    boat = mesh_object("PaperBoat", vertices, faces, paper)
    boat.scale = (0.75, 0.75, 0.75)
    return boat


def gate(x, label, index):
    """A square frame that stands in the water, with its name above it."""
    glow = material(f"Gate{index}", BLUE, roughness=0.4, emission=1.2)
    bars = [
        ((x, -1.6, 1.3), (0.09, 0.09, 2.6)),
        ((x, 1.6, 1.3), (0.09, 0.09, 2.6)),
        ((x, 0.0, 2.6), (0.09, 3.29, 0.09)),
    ]
    for location, size in bars:
        bpy.ops.mesh.primitive_cube_add(size=1, location=location)
        bar = bpy.context.object
        bar.scale = size
        bar.data.materials.append(glow)
    bpy.ops.object.text_add(location=(x, 1.6, 2.95), rotation=(math.radians(90), 0, 0))
    text = bpy.context.object
    text.data.body = label
    text.data.align_x = "CENTER"
    text.data.size = 0.52
    text.data.materials.append(glow)
    return glow


def light_gate(glow, pass_frame):
    """The gate turns from blue to green when the boat passes."""
    shader = glow.node_tree.nodes["Principled BSDF"]
    for frame, color, strength in [
        (pass_frame - 6, BLUE, 1.2), (pass_frame + 2, GREEN, 9.0), (pass_frame + 26, GREEN, 3.0),
    ]:
        for name, value in [("Emission Color", color), ("Base Color", color), ("Emission Strength", strength)]:
            shader.inputs[name].default_value = value
            shader.inputs[name].keyframe_insert("default_value", frame=frame)


def fish(index, x, y, jump_frame):
    """A small fish that jumps over the water in an arc."""
    scales = material(f"Fish{index}", (0.95, 0.35, 0.08, 1.0), roughness=0.3, emission=0.6)
    bpy.ops.mesh.primitive_uv_sphere_add(segments=16, ring_count=8, radius=0.2, location=(x, y, -1.2))
    body = bpy.context.object
    body.scale = (1.9, 0.55, 0.9)
    body.data.materials.append(scales)
    bpy.ops.mesh.primitive_cone_add(vertices=4, radius1=0.2, depth=0.3, location=(-0.24, 0, 0))
    tail = bpy.context.object
    tail.rotation_euler = (0, math.radians(90), 0)
    tail.scale = (1.0, 0.25, 1.0)
    tail.data.materials.append(scales)
    tail.parent = body
    steps = 12
    for step in range(steps + 1):
        share = step / steps
        frame = jump_frame + step * 2
        body.location = (x + share * 1.7, y, -0.9 + math.sin(share * math.pi) * 1.75)
        body.rotation_euler = (0, math.radians(-55 + share * 110), 0)
        body.keyframe_insert("location", frame=frame)
        body.keyframe_insert("rotation_euler", frame=frame)


def title_text(body, size, z, frames, delay):
    glow = material(f"Title{delay}", (1, 1, 1, 1), emission=2.2)
    bpy.ops.object.text_add(location=(9.6, 2.5, z - 0.9), rotation=(math.radians(90), 0, 0))
    text = bpy.context.object
    text.data.body = body
    text.data.align_x = "CENTER"
    text.data.size = size
    text.data.extrude = 0.02
    text.data.materials.append(glow)
    start = int(frames * SAIL_END_SHARE) - 14 + delay
    for frame, height, scale in [(1, z - 0.9, 0.0), (start, z - 0.9, 0.0), (start + 22, z, 1.0)]:
        text.location.z = height
        text.scale = (scale, scale, scale)
        text.keyframe_insert("location", frame=frame)
        text.keyframe_insert("scale", frame=frame)


def boat_x(frame, frames):
    share = min(1.0, (frame - 1) / (frames - 1))
    eased = share * share * (3 - 2 * share) * 0.25 + share * 0.75
    return BOAT_START_X + (BOAT_END_X - BOAT_START_X) * eased


def animate_boat(boat, frames):
    for frame in range(1, frames + 1, 3):
        boat.location = (boat_x(frame, frames), 0.0, 0.1 + math.sin(frame * 0.16) * 0.045)
        boat.rotation_euler = (math.sin(frame * 0.11) * 0.06, math.sin(frame * 0.16 + 1.0) * 0.05, 0.0)
        boat.keyframe_insert("location", frame=frame)
        boat.keyframe_insert("rotation_euler", frame=frame)


def camera_rig(frames):
    """The camera follows the boat from the side, then settles for the title."""
    bpy.ops.object.empty_add(location=(BOAT_START_X + 2.0, 0.0, 1.2))
    target = bpy.context.object
    bpy.ops.object.camera_add(location=(BOAT_START_X + 1.0, -9.5, 2.3))
    camera = bpy.context.object
    camera.data.lens = 32
    constraint = camera.constraints.new("TRACK_TO")
    constraint.target = target
    constraint.track_axis = "TRACK_NEGATIVE_Z"
    constraint.up_axis = "UP_Y"
    settle = int(frames * SAIL_END_SHARE)
    keys = [
        (1, BOAT_START_X + 2.0, (BOAT_START_X + 0.5, -9.5, 2.3)),
        (settle, 8.4, (8.6, -12.5, 2.9)),
        (frames, 9.6, (9.6, -13.2, 3.1)),
    ]
    for frame, target_x, location in keys:
        target.location = (target_x, 0.0, 1.5)
        camera.location = location
        target.keyframe_insert("location", frame=frame)
        camera.keyframe_insert("location", frame=frame)
    bpy.context.scene.camera = camera


def build_scene(frames, fps):
    bpy.ops.wm.read_factory_settings(use_empty=True)
    scene = bpy.context.scene
    scene.frame_start = 1
    scene.frame_end = frames
    scene.render.fps = fps

    world = bpy.data.worlds.new("Night")
    world.use_nodes = True
    world.node_tree.nodes["Background"].inputs["Color"].default_value = SKY
    scene.world = world

    bpy.ops.mesh.primitive_plane_add(size=400, location=(0, 0, 0))
    bpy.context.object.data.materials.append(material("Water", WATER, roughness=0.12, metallic=0.85))

    boat = paper_boat(material("Paper", PAPER, roughness=0.85, emission=0.25))
    animate_boat(boat, frames)
    for index, (x, label) in enumerate(GATES):
        glow = gate(x, label, index)
        pass_frame = next(f for f in range(1, frames + 1) if boat_x(f, frames) >= x)
        light_gate(glow, pass_frame)
    fish(0, -5.6, 1.1, int(frames * 0.2))
    fish(1, 1.2, 1.4, int(frames * 0.44))

    title_text("Site Reliability Clanker", 1.25, 4.35, frames, 0)
    title_text("The model proposes. Code decides.", 0.5, 3.5, frames, 10)

    bpy.ops.object.light_add(type="SUN", location=(0, -6, 10), rotation=(math.radians(50), 0, math.radians(20)))
    bpy.context.object.data.energy = 2.2
    bpy.ops.object.light_add(type="AREA", location=(2, -7, 6))
    bpy.context.object.data.energy = 900
    bpy.context.object.data.size = 14
    camera_rig(frames)
    return scene


def configure_render(scene, out_dir):
    engines = [item.identifier for item in bpy.types.RenderSettings.bl_rna.properties["engine"].enum_items]
    eevee = next((name for name in engines if "EEVEE" in name), None)
    if eevee is None:
        raise RuntimeError(f"this Blender has no Eevee render engine (found {engines})")
    scene.render.engine = eevee
    scene.render.resolution_x = 1920
    scene.render.resolution_y = 1080
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = "PNG"
    scene.render.filepath = f"{out_dir}/frame-"
    scene.eevee.taa_render_samples = 16
    if hasattr(scene.eevee, "use_raytracing"):
        scene.eevee.use_raytracing = True


def main():
    options = arguments()
    scene = build_scene(options.frames, options.fps)
    configure_render(scene, options.out)
    if options.only:
        scene.frame_set(options.only)
        scene.render.filepath = f"{options.out}/test-{options.only:04d}"
        bpy.ops.render.render(write_still=True)
    else:
        bpy.ops.render.render(animation=True)


main()
