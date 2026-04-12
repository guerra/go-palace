# User model used by backend sample tests.
class User:
    def __init__(self, name):
        self.name = name

    def display(self):
        return f'User({self.name})'


# end of fixture
