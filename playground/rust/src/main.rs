// hello-demo: open a CS2 .dem and print header, players, tick-0 positions,
// and the positions on the first live-round tick.
//
// Backed by the Rust crate `parser` (locally renamed `demoparser`) from
// https://github.com/LaihoE/demoparser. The Rust crate is much lower-level
// than the Python `demoparser2` wrapper: there is no `DemoParser` class with
// `parse_header()` / `parse_ticks()` / `parse_event()` methods. Instead you
// build a `ParserInputs` struct describing what you want (props, events,
// ticks) and call `Parser::parse_demo` to get a `DemoOutput`.
//
// Live-round detection mirrors the Python playground:
//   - find begin_new_match (fires at tick 0 as a demo-start artifact AND at
//     the real match start — take the MAX tick)
//   - find round_freeze_end events whose tick > that floor
//   - the smallest such tick is the first live round's freeze-end
//
// Cross-library reference:
//   - Python (demoparser2): map = de_mirage, 10 players, first live tick = 4238
//   - Go (demoinfocs):      map = de_mirage, 11 players (incl SourceTV),
//                           first live tick = 2543

use ahash::AHashMap;
use demoparser::first_pass::parser_settings::{FirstPassParser, ParserInputs, create_mmap};
use demoparser::parse_demo::{Parser, ParsingMode};
use demoparser::second_pass::parser_settings::create_huffman_lookup_table;
use demoparser::second_pass::variants::VarVec;
use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 2 {
        eprintln!("Usage: hello-demo <path-to-dem>");
        return ExitCode::from(1);
    }
    let demo_path = &args[1];

    let mmap = match create_mmap(demo_path.clone()) {
        Ok(m) => m,
        Err(e) => {
            eprintln!("failed to open demo {demo_path}: {e}");
            return ExitCode::from(1);
        }
    };
    let bytes: &[u8] = &mmap;

    // Huffman lookup table is shared across all parser invocations.
    let huf = create_huffman_lookup_table();

    // --- Header ---
    let header = match parse_header(bytes, &huf) {
        Ok(h) => h,
        Err(e) => {
            eprintln!("failed to parse header: {e}");
            return ExitCode::from(1);
        }
    };
    println!("== Header ==");
    println!(
        "Map:          {}",
        header
            .get("map_name")
            .map(String::as_str)
            .unwrap_or("<unknown>")
    );
    println!(
        "Server:       {}",
        header
            .get("server_name")
            .map(String::as_str)
            .unwrap_or("<unknown>")
    );
    println!(
        "Demo type:    {}",
        header
            .get("demo_version_name")
            .map(String::as_str)
            .unwrap_or("<unknown>")
    );

    // --- Players ---
    // player_md is populated as part of normal parsing; only_header=true
    // still walks far enough to collect it.
    let players = match parse_player_md(bytes, &huf) {
        Ok(p) => p,
        Err(e) => {
            eprintln!("failed to parse player info: {e}");
            return ExitCode::from(1);
        }
    };
    println!("\n== Players ==");
    for p in &players {
        let name = p.name.clone().unwrap_or_else(|| "<unknown>".into());
        let steamid = p
            .steamid
            .map(|s| s.to_string())
            .unwrap_or_else(|| "<unknown>".into());
        println!("  - {name}  ({steamid})");
    }
    println!("  ({} total)", players.len());

    // --- Tick 0 ---
    println!("\n== Tick 0 (raw first tick) ==");
    match parse_positions_at_tick(bytes, &huf, 0) {
        Ok(rows) if rows.is_empty() => {
            println!("  (no rows returned for tick 0 — players may not be spawned yet)");
        }
        Ok(rows) => {
            for row in rows {
                print_position_row(&row);
            }
        }
        Err(e) => {
            eprintln!("  (failed to parse tick 0 positions: {e})");
        }
    }

    // --- First live-round tick ---
    println!("\n== First live-round tick ==");
    let events = match parse_events(bytes, &huf) {
        Ok(e) => e,
        Err(e) => {
            eprintln!("  (failed to parse events: {e})");
            return ExitCode::from(0);
        }
    };

    let freeze_end_ticks: Vec<i32> = events
        .iter()
        .filter(|e| e.name == "round_freeze_end")
        .map(|e| e.tick)
        .collect();
    if freeze_end_ticks.is_empty() {
        println!("  (no round_freeze_end events found — cannot determine first live tick)");
        return ExitCode::from(0);
    }

    let begin_match_ticks: Vec<i32> = events
        .iter()
        .filter(|e| e.name == "begin_new_match")
        .map(|e| e.tick)
        .collect();
    let (match_start_tick, anchor_note) = if let Some(&max) = begin_match_ticks.iter().max() {
        (
            max,
            format!("first round_freeze_end after begin_new_match at tick {max}"),
        )
    } else {
        (
            0,
            "begin_new_match not found; using smallest round_freeze_end".to_string(),
        )
    };

    let live_freezes: Vec<i32> = freeze_end_ticks
        .iter()
        .copied()
        .filter(|t| *t > match_start_tick)
        .collect();
    let first_live_tick = if let Some(&min) = live_freezes.iter().min() {
        min
    } else {
        eprintln!(
            "  WARNING: no round_freeze_end after begin_new_match (tick {match_start_tick}); falling back to global minimum"
        );
        *freeze_end_ticks.iter().min().expect("non-empty")
    };

    println!("Tick: {first_live_tick}  ({anchor_note})");
    match parse_positions_at_tick(bytes, &huf, first_live_tick) {
        Ok(rows) if rows.is_empty() => {
            println!("  (no position rows at that tick)");
        }
        Ok(rows) => {
            for row in rows {
                print_position_row(&row);
            }
        }
        Err(e) => {
            eprintln!("  (failed to parse positions at tick {first_live_tick}: {e})");
        }
    }

    ExitCode::from(0)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

struct PositionRow {
    name: String,
    x: f32,
    y: f32,
    z: f32,
}

fn print_position_row(row: &PositionRow) {
    println!(
        "  {:<24}  x={:>8.1}  y={:>8.1}  z={:>8.1}",
        row.name, row.x, row.y, row.z
    );
}

fn base_inputs<'a>(huf: &'a Vec<(u8, u8)>) -> ParserInputs<'a> {
    ParserInputs {
        real_name_to_og_name: AHashMap::default(),
        wanted_players: vec![],
        wanted_player_props: vec![],
        wanted_other_props: vec![],
        wanted_prop_states: AHashMap::default(),
        wanted_ticks: vec![],
        wanted_events: vec![],
        parse_ents: false,
        parse_projectiles: false,
        parse_grenades: false,
        only_header: true,
        only_convars: false,
        huffman_lookup_table: huf,
        order_by_steamid: false,
        list_props: false,
        fallback_bytes: None,
    }
}

fn parse_header(bytes: &[u8], huf: &Vec<(u8, u8)>) -> Result<AHashMap<String, String>, String> {
    let inputs = base_inputs(huf);
    let mut first_pass = FirstPassParser::new(&inputs);
    first_pass
        .parse_header_only(bytes)
        .map_err(|e| format!("{e}"))
}

fn parse_player_md(
    bytes: &[u8],
    huf: &Vec<(u8, u8)>,
) -> Result<Vec<demoparser::second_pass::parser_settings::PlayerEndMetaData>, String> {
    let inputs = base_inputs(huf);
    let mut parser = Parser::new(inputs, ParsingMode::Normal);
    let output = parser.parse_demo(bytes).map_err(|e| format!("{e}"))?;
    Ok(output.player_md)
}

fn parse_events(
    bytes: &[u8],
    huf: &Vec<(u8, u8)>,
) -> Result<Vec<demoparser::second_pass::game_events::GameEvent>, String> {
    let mut inputs = base_inputs(huf);
    inputs.only_header = false;
    inputs.wanted_events = vec![
        "round_freeze_end".to_string(),
        "begin_new_match".to_string(),
    ];
    inputs.parse_ents = true;
    let mut parser = Parser::new(inputs, ParsingMode::Normal);
    let output = parser.parse_demo(bytes).map_err(|e| format!("{e}"))?;
    Ok(output.game_events)
}

fn parse_positions_at_tick(
    bytes: &[u8],
    huf: &Vec<(u8, u8)>,
    tick: i32,
) -> Result<Vec<PositionRow>, String> {
    let mut inputs = base_inputs(huf);
    inputs.only_header = false;
    inputs.wanted_player_props = vec![
        "X".to_string(),
        "Y".to_string(),
        "Z".to_string(),
        "name".to_string(),
    ];
    // The Python wrapper passes the user-friendly names through unchanged so
    // they show up as column names; do the same for parity.
    let mut name_map = AHashMap::default();
    for n in &inputs.wanted_player_props {
        name_map.insert(n.clone(), n.clone());
    }
    inputs.real_name_to_og_name = name_map;
    inputs.wanted_ticks = vec![tick];
    inputs.parse_ents = true;

    let mut parser = Parser::new(inputs, ParsingMode::Normal);
    let output = parser.parse_demo(bytes).map_err(|e| format!("{e}"))?;

    // Find the prop ids for X/Y/Z/name within prop_controller.prop_infos.
    let mut x_id: Option<u32> = None;
    let mut y_id: Option<u32> = None;
    let mut z_id: Option<u32> = None;
    let mut name_id: Option<u32> = None;
    for info in &output.prop_controller.prop_infos {
        match info.prop_friendly_name.as_str() {
            "X" => x_id = Some(info.id),
            "Y" => y_id = Some(info.id),
            "Z" => z_id = Some(info.id),
            "name" => name_id = Some(info.id),
            _ => {}
        }
    }

    let (x_id, y_id, z_id, name_id) = match (x_id, y_id, z_id, name_id) {
        (Some(x), Some(y), Some(z), Some(n)) => (x, y, z, n),
        _ => {
            return Err(format!(
                "missing one of X/Y/Z/name in prop_infos (x={x_id:?} y={y_id:?} z={z_id:?} name={name_id:?})",
            ));
        }
    };

    // df is AHashMap<u32, PropColumn>. Each PropColumn has a VarVec aligned
    // across props — index i in X corresponds to the same row as index i in
    // Y, Z, name. With wanted_ticks restricted to a single tick, each row
    // represents one player at that tick.
    let xs = column_as_f32(&output.df, x_id);
    let ys = column_as_f32(&output.df, y_id);
    let zs = column_as_f32(&output.df, z_id);
    let names = column_as_string(&output.df, name_id);

    let mut rows = vec![];
    let n_rows = [xs.len(), ys.len(), zs.len(), names.len()]
        .iter()
        .copied()
        .min()
        .unwrap_or(0);
    for i in 0..n_rows {
        let name = names[i].clone().unwrap_or_else(|| "<unknown>".into());
        let x = xs[i].unwrap_or(f32::NAN);
        let y = ys[i].unwrap_or(f32::NAN);
        let z = zs[i].unwrap_or(f32::NAN);
        rows.push(PositionRow { name, x, y, z });
    }
    Ok(rows)
}

fn column_as_f32(
    df: &AHashMap<u32, demoparser::second_pass::variants::PropColumn>,
    id: u32,
) -> Vec<Option<f32>> {
    match df.get(&id).and_then(|c| c.data.as_ref()) {
        Some(VarVec::F32(v)) => v.clone(),
        _ => vec![],
    }
}

fn column_as_string(
    df: &AHashMap<u32, demoparser::second_pass::variants::PropColumn>,
    id: u32,
) -> Vec<Option<String>> {
    match df.get(&id).and_then(|c| c.data.as_ref()) {
        Some(VarVec::String(v)) => v.clone(),
        _ => vec![],
    }
}
