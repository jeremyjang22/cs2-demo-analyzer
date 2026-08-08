import pandas as pd
from pathlib import Path

if __name__ == "__main__":

    script_dir = Path(__file__).resolve().parent
    file_path = script_dir / ".." / ".." / ".." / ".." / "out" / "05-11-2026_mirage_44-32-10" / "players.csv"
    file_path = file_path.resolve()

    print(file_path)  # Check the resolved path

    '''
    get player steam id to track movement
    '''
    players_df = pd.read_csv(file_path)

    players = players_df['name'].unique()
    print(players)

    player_to_track = 'solitude'
    player_steamid = ""

    for i, row in players_df.iterrows():
        # print(i, "-"*100)
        # print(row.keys())

        if row['name'] == player_to_track:
            player_steamid = row['steamid']
    
    print(f"{player_to_track}: {player_steamid}")

    '''
    get round data
    '''

    rounds_filepath = script_dir / ".." / ".." / ".." / ".." / "out" / "05-11-2026_mirage_44-32-10" / "rounds.csv"
    rounds_filepath = rounds_filepath.resolve()

    rounds_df = pd.read_csv(rounds_filepath)
    print(f"rounds_df shape: {rounds_df.shape}")


    for i, row in rounds_df.iterrows():
        pass
        # print()

    print(rounds_df.iloc[0])

    '''
    get ticks for round
    '''
    ticks_filepath = script_dir / ".." / ".." / ".." / ".." / "out" / "05-11-2026_mirage_44-32-10" / "ticks.csv.gz"
    ticks_filepath = ticks_filepath.resolve()
    
    ticks_df = pd.read_csv(ticks_filepath)
    print(f"ticks_df shape: {ticks_df.shape}")
    print(ticks_df.iloc[0])

    # query (use duckdb at scale)
    player_round_0 = ticks_df.query("steamid==@player_steamid and round==1 and is_alive==1")
    print(f"tracking {player_to_track} for round 1 (shape): {player_round_0.shape}")


    # print(players_df.to_e())